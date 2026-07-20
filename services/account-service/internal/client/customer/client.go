// Package customer is a small HTTP client to Customer Service's internal
// KYC-tier lookup — see services/customer-service/internal/transport/http/handlers.go's
// GetKYCTierByUserID for why this exists: Auth Service's JWT kyc_tier claim
// is a documented Phase 1/2 stand-in that's always 0, so FR-ACC-1's Tier-1+
// gate needs a live lookup instead of trusting that claim.
package customer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{baseURL: baseURL, httpClient: &http.Client{Timeout: 3 * time.Second}}
}

type kycTierResponse struct {
	Data struct {
		KYCTier   int    `json:"kycTier"`
		KYCStatus string `json:"kycStatus"`
	} `json:"data"`
}

// GetKYCTier returns the caller's current KYC tier, or 0 if they have no
// customer profile yet (treated the same as Tier 0 — unverified — rather
// than an error, since "no profile yet" and "profile exists at tier 0"
// should be handled identically by every caller of this method).
func (c *Client) GetKYCTier(ctx context.Context, userID string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/customers/by-user/"+userID+"/kyc-tier", nil)
	if err != nil {
		return 0, fmt.Errorf("building request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("calling customer-service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return 0, nil
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("customer-service returned status %d", resp.StatusCode)
	}

	var body kycTierResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("decoding response: %w", err)
	}
	return body.Data.KYCTier, nil
}
