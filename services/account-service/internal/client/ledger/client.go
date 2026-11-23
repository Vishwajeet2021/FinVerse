// Package ledger wraps the generated ledgerv1 gRPC client so
// internal/service never imports it directly, per docs/12-coding-standards.md §1.1.
package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/finverse/account-service/internal/service"
	ledgerv1 "github.com/finverse/shared/genproto/ledger/v1"
	"github.com/finverse/shared/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	grpcClient ledgerv1.LedgerServiceClient
	conn       *grpc.ClientConn
}

// NewClient dials Transaction Service. Plaintext internal gRPC is the
// documented Phase 2 posture — mTLS is a Phase 4 AWS concern
// (docs/11-security-standards.md §7), not built yet.
//
// conn.Connect() kicks off the connection handshake immediately rather than
// lazily on the first RPC (grpc.NewClient's default behavior): without it,
// the first real request after startup pays for connection setup inside its
// own per-call timeout, which measurably caused the first GetAccountHistory
// call after a fresh deploy to time out in local testing. Connect() doesn't
// block — it just starts the dial in the background so it's warm by the
// time an actual request needs it.
func NewClient(addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithStatsHandler(telemetry.GRPCClientStats()))
	if err != nil {
		return nil, fmt.Errorf("dialing transaction-service at %s: %w", addr, err)
	}
	conn.Connect()
	return &Client{grpcClient: ledgerv1.NewLedgerServiceClient(conn), conn: conn}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) GetAccountHistory(ctx context.Context, participantID string, from, to *time.Time, limit int, cursor string) ([]service.StatementEntry, string, error) {
	req := &ledgerv1.GetAccountHistoryRequest{
		ParticipantId: participantID,
		Limit:         int32(limit), //nolint:gosec // limit is caller-bounded (service.statementPageSize = 200), never near int32 overflow
		Cursor:        cursor,
	}
	if from != nil {
		req.From = from.Format(time.RFC3339)
	}
	if to != nil {
		req.To = to.Format(time.RFC3339)
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := c.grpcClient.GetAccountHistory(ctx, req)
	if err != nil {
		return nil, "", fmt.Errorf("calling GetAccountHistory: %w", err)
	}

	entries := make([]service.StatementEntry, len(resp.GetEntries()))
	for i, e := range resp.GetEntries() {
		entries[i] = service.StatementEntry{
			TransactionID: e.GetTransactionId(),
			LegID:         e.GetLegId(),
			AmountMinor:   e.GetAmountMinor(),
			Direction:     e.GetDirection().String(),
			Currency:      e.GetCurrency(),
		}
	}
	return entries, resp.GetNextCursor(), nil
}
