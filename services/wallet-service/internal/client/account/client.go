// Package account asks Account Service whether a wallet's funding account may be debited.
package account

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/finverse/shared/svcauth"
	"github.com/finverse/wallet-service/internal/domain"
)

// statusTTL bounds how long "may be debited" is reused (docs/23 gap list). A frozen or closed
// answer is never cached, so a refusal is immediate and stays in force.
const statusTTL = 3 * time.Second

type Client struct {
	baseURL    string
	httpClient *http.Client
	signer     *svcauth.Signer

	mu        sync.Mutex
	debitable map[string]time.Time
}

func NewClient(baseURL string, signer *svcauth.Signer) *Client {
	return &Client{baseURL: baseURL, signer: signer, httpClient: &http.Client{Timeout: 3 * time.Second}, debitable: map[string]time.Time{}}
}

type statusResponse struct {
	Data struct {
		Status string `json:"status"`
	} `json:"data"`
}

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
	var body statusResponse
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
		return domain.ErrFundingAccountFrozen
	case "CLOSED":
		return domain.ErrFundingAccountClosed
	default:
		return fmt.Errorf("account-service reported unknown status %q", body.Data.Status)
	}
}
