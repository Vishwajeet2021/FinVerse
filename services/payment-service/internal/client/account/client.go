// Package account is a small HTTP client to Account Service's internal
// account-number lookup — see
// services/account-service/internal/transport/http/handlers.go's
// ResolveAccountNumber for why this exists: a payer knows the recipient's
// public account number, never Account Service's internal UUID, but the
// ledger's participant_id column (and every gRPC leg Payment Service posts)
// requires that UUID.
package account

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/finverse/shared/svcauth"
	"net/http"
	"sync"
	"time"

	"github.com/finverse/payment-service/internal/domain"
)

// statusTTL is how long "this account may be debited" is reused. It bounds how long a freeze
// can take to stop payments from a given Payment Service replica (docs/23 gap list). A frozen or
// closed answer is never cached, so the first refusal is immediate and stays in force.
const statusTTL = 3 * time.Second

// ownershipTTL bounds how long a positive ownership answer is reused. Holding
// an account changes rarely, and this keeps the extra Account Service hop off
// most requests; the cost is that a holder removed within the last minute can
// still initiate a payment for up to a minute.
const ownershipTTL = 60 * time.Second

type Client struct {
	baseURL    string
	httpClient *http.Client
	signer     *svcauth.Signer

	mu        sync.Mutex
	ownership map[string]time.Time // userID|accountID -> verified until
	debitable map[string]time.Time // accountID -> known debitable until
}

func NewClient(baseURL string, signer *svcauth.Signer) *Client {
	return &Client{baseURL: baseURL, signer: signer, httpClient: &http.Client{Timeout: 3 * time.Second}, ownership: map[string]time.Time{}, debitable: map[string]time.Time{}}
}

// VerifyOwnership confirms the caller holds accountID by asking Account Service
// with the caller's own bearer token: GET /api/v1/accounts/{id} already enforces
// holder ownership (docs/10-authentication-authorization.md 3.3), so this reuses
// that rule instead of duplicating it. Only successes are cached.
func (c *Client) VerifyOwnership(ctx context.Context, userID, bearer, accountID string) error {
	key := userID + "|" + accountID
	c.mu.Lock()
	until, ok := c.ownership[key]
	c.mu.Unlock()
	if ok && time.Now().Before(until) {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/accounts/"+accountID, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling account-service: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		c.mu.Lock()
		c.ownership[key] = time.Now().Add(ownershipTTL)
		c.mu.Unlock()
		return nil
	case http.StatusForbidden, http.StatusNotFound:
		return domain.ErrNotAccountHolder
	default:
		return fmt.Errorf("account-service returned status %d verifying ownership", resp.StatusCode)
	}
}

type resolveResponse struct {
	Data struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"data"`
}

// ResolveAccountID turns an account number into the internal account ID
// Transaction Service's ledger expects as participant_id. Returns
// domain.ErrRecipientAccountNotFound if no account has that number.
func (c *Client) ResolveAccountID(ctx context.Context, accountNumber string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/accounts/by-number/"+accountNumber, nil)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}
	c.signer.SignRequest(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling account-service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", domain.ErrRecipientAccountNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("account-service returned status %d", resp.StatusCode)
	}

	var body resolveResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}
	return body.Data.ID, nil
}

// CheckCanDebit asks Account Service (service-token route) whether the account may be debited.
// It is called for every payment, scheduled and standing ones included, because those run with
// no user token and must still respect a freeze.
func (c *Client) CheckCanDebit(ctx context.Context, accountID string) error {
	c.mu.Lock()
	until, ok := c.debitable[accountID]
	c.mu.Unlock()
	if ok && time.Now().Before(until) {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/account-status/"+accountID, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	c.signer.SignRequest(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling account-service: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("account-service returned status %d for account status", resp.StatusCode)
	}
	var body resolveResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("decoding account status: %w", err)
	}
	switch body.Data.Status {
	case "ACTIVE":
		c.mu.Lock()
		c.debitable[accountID] = time.Now().Add(statusTTL)
		c.mu.Unlock()
		return nil
	case "FROZEN":
		return domain.ErrSourceAccountFrozen
	case "CLOSED":
		return domain.ErrSourceAccountClosed
	default:
		return fmt.Errorf("account-service reported unknown status %q", body.Data.Status)
	}
}
