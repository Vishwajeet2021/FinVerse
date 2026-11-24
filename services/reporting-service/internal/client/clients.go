// Package client holds Reporting Service's clients for the services it reads from.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/finverse/reporting-service/internal/service"
	ledgerv1 "github.com/finverse/shared/genproto/ledger/v1"
	"github.com/finverse/shared/svcauth"
	"github.com/finverse/shared/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Ledger reads account history from Transaction Service over gRPC, authenticated with a
// service token (docs/22 D-S1; "reporting-service" is on the allow-list).
type Ledger struct {
	c    ledgerv1.LedgerServiceClient
	conn *grpc.ClientConn
}

func NewLedger(addr string, signer *svcauth.Signer) (*Ledger, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(signer.UnaryClientInterceptor()), grpc.WithStatsHandler(telemetry.GRPCClientStats()))
	if err != nil {
		return nil, fmt.Errorf("dialing transaction-service at %s: %w", addr, err)
	}
	conn.Connect()
	return &Ledger{c: ledgerv1.NewLedgerServiceClient(conn), conn: conn}, nil
}

func (l *Ledger) Close() error { return l.conn.Close() }

func (l *Ledger) History(ctx context.Context, accountID string, from, to *time.Time, limit int, cursor string) ([]service.Entry, string, error) {
	req := &ledgerv1.GetAccountHistoryRequest{ParticipantId: accountID, Limit: int32(limit), Cursor: cursor} //nolint:gosec // limit is service.PageSize
	if from != nil {
		req.From = from.UTC().Format(time.RFC3339Nano)
	}
	if to != nil {
		req.To = to.UTC().Format(time.RFC3339Nano)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := l.c.GetAccountHistory(ctx, req)
	if err != nil {
		return nil, "", fmt.Errorf("GetAccountHistory: %w", err)
	}
	out := make([]service.Entry, 0, len(resp.GetEntries()))
	for _, e := range resp.GetEntries() {
		at, err := time.Parse(time.RFC3339Nano, e.GetCreatedAt())
		if err != nil {
			// Without a timestamp a leg cannot be placed on a statement; fail the report
			// (retryably, in case Transaction Service is mid-upgrade) rather than guess.
			return nil, "", fmt.Errorf("ledger entry %s has no usable created_at", e.GetLegId())
		}
		dir := ""
		switch e.GetDirection() {
		case ledgerv1.Direction_DIRECTION_DEBIT:
			dir = "D"
		case ledgerv1.Direction_DIRECTION_CREDIT:
			dir = "C"
		}
		out = append(out, service.Entry{TransactionID: e.GetTransactionId(), LegID: e.GetLegId(),
			AmountMinor: e.GetAmountMinor(), Direction: dir, Currency: e.GetCurrency(), At: at})
	}
	return out, resp.GetNextCursor(), nil
}

var httpClient = &http.Client{Timeout: 5 * time.Second}

// Accounts asks Account Service whether the bearer's owner holds an account, by forwarding the
// caller's own token to the ordinary owner-checked endpoint (docs/23 D-R5). Account Service
// remains the only authority on holdership.
type Accounts struct{ base string }

func NewAccounts(base string) *Accounts { return &Accounts{base: base} }

func (a *Accounts) CanAccess(ctx context.Context, bearer, accountID string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+"/api/v1/accounts/"+url.PathEscape(accountID), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusOK:
		return true, nil
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("account-service answered %d", resp.StatusCode)
	}
}

// Fraud reads cases with the administrator's own token.
type Fraud struct{ base string }

func NewFraud(base string) *Fraud { return &Fraud{base: base} }

func (f *Fraud) OpenAndResolvedCases(ctx context.Context, bearer string) ([]service.FraudCase, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.base+"/api/v1/fraud/cases?limit=200", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fraud-service answered %d", resp.StatusCode)
	}
	var env struct {
		Data []service.FraudCase `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decoding fraud cases: %w", err)
	}
	return env.Data, nil
}
