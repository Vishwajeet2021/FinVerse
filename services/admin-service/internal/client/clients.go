// Package client holds Admin Service's HTTP clients for the services that perform the actions.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/finverse/admin-service/internal/service"
	"github.com/finverse/shared/svcauth"
)

const maxBody = 1 << 20 // downstream answers are small; refuse to buffer more than 1 MiB

var httpClient = &http.Client{Timeout: 5 * time.Second}

// call performs a request and turns any non-2xx answer into a *service.DownstreamError that
// carries the downstream error code but never its message text (which may be user-facing
// copy meant for a different audience).
func call(ctx context.Context, req *http.Request) (json.RawMessage, error) {
	resp, err := httpClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", req.URL.Host, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("reading answer from %s: %w", req.URL.Host, err)
	}
	if resp.StatusCode/100 != 2 {
		var env struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &env)
		code := env.Error.Code
		if code == "" {
			code = "UNKNOWN"
		}
		return nil, &service.DownstreamError{Status: resp.StatusCode, Code: code}
	}
	return body, nil
}

func jsonRequest(method, u string, body any) (*http.Request, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// Accounts calls Account Service's internal admin routes with a service token.
type Accounts struct {
	base   string
	signer *svcauth.Signer
}

func NewAccounts(base string, s *svcauth.Signer) *Accounts { return &Accounts{base: base, signer: s} }

func (a *Accounts) post(ctx context.Context, accountID, verb string, body any) error {
	req, err := jsonRequest(http.MethodPost, a.base+"/internal/admin/accounts/"+url.PathEscape(accountID)+"/"+verb, body)
	if err != nil {
		return err
	}
	a.signer.SignRequest(req)
	_, err = call(ctx, req)
	return err
}

func (a *Accounts) Freeze(ctx context.Context, accountID, reason string) error {
	return a.post(ctx, accountID, "freeze", map[string]string{"reason": reason})
}

func (a *Accounts) Unfreeze(ctx context.Context, accountID string) error {
	return a.post(ctx, accountID, "unfreeze", nil)
}

// Fraud forwards the administrator's own bearer token (docs/23 D-A3).
type Fraud struct{ base string }

func NewFraud(base string) *Fraud { return &Fraud{base: base} }

func (f *Fraud) OpenCases(ctx context.Context, bearer string, limit int) (json.RawMessage, int, error) {
	req, err := jsonRequest(http.MethodGet, f.base+"/api/v1/fraud/cases?status=OPEN&limit=200", nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	body, err := call(ctx, req)
	if err != nil {
		return nil, 0, err
	}
	var env struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, 0, fmt.Errorf("decoding fraud cases: %w", err)
	}
	total := len(env.Data) // capped at the 200 requested; the page shows "200+" beyond that
	recent := env.Data
	if len(recent) > limit {
		recent = recent[:limit]
	}
	out, _ := json.Marshal(recent)
	if recent == nil {
		out = []byte("[]")
	}
	return out, total, nil
}

func (f *Fraud) Resolve(ctx context.Context, bearer, caseID, outcome string) error {
	req, err := jsonRequest(http.MethodPost, f.base+"/api/v1/fraud/cases/"+url.PathEscape(caseID)+"/resolve", map[string]string{"outcome": outcome})
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	_, err = call(ctx, req)
	return err
}

type Customers struct{ base string }

func NewCustomers(base string) *Customers { return &Customers{base: base} }

func (c *Customers) ReviewKYC(ctx context.Context, bearer, customerID string, approve bool, tier int) error {
	req, err := jsonRequest(http.MethodPost, c.base+"/api/v1/customers/"+url.PathEscape(customerID)+"/kyc/review",
		map[string]any{"approve": approve, "tier": tier})
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	_, err = call(ctx, req)
	return err
}

// Prometheus reads instant queries and the alert list over its HTTP API.
type Prometheus struct{ base string }

func NewPrometheus(base string) *Prometheus { return &Prometheus{base: base} }

type promResult struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]any            `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func (p *Prometheus) query(ctx context.Context, q string) (*promResult, error) {
	req, err := http.NewRequest(http.MethodGet, p.base+"/api/v1/query?query="+url.QueryEscape(q), nil)
	if err != nil {
		return nil, err
	}
	body, err := call(ctx, req)
	if err != nil {
		return nil, err
	}
	var r promResult
	if err := json.Unmarshal(body, &r); err != nil || r.Status != "success" {
		return nil, fmt.Errorf("prometheus query failed")
	}
	return &r, nil
}

func (p *Prometheus) Scalar(ctx context.Context, q string) (float64, error) {
	r, err := p.query(ctx, q)
	if err != nil {
		return 0, err
	}
	if len(r.Data.Result) == 0 {
		return 0, nil // no series yet (e.g. no payments since start) is a genuine zero
	}
	s, _ := r.Data.Result[0].Value[1].(string)
	var v float64
	if _, err := fmt.Sscanf(s, "%g", &v); err != nil {
		return 0, fmt.Errorf("prometheus returned a non-numeric value")
	}
	return v, nil
}

func (p *Prometheus) Firing(ctx context.Context) ([]string, error) {
	r, err := p.query(ctx, `ALERTS{alertstate="firing"}`)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, res := range r.Data.Result {
		n := res.Metric["alertname"]
		if l := res.Metric["service"]; l != "" {
			n += " (" + l + ")"
		}
		names = append(names, n)
	}
	return names, nil
}
