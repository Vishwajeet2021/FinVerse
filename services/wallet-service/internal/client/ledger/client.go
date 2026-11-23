// Package ledger wraps the generated ledgerv1 gRPC client so
// internal/service never imports it directly, per docs/12-coding-standards.md §1.1.
package ledger

import (
	"context"
	"fmt"
	"time"

	ledgerv1 "github.com/finverse/shared/genproto/ledger/v1"
	"github.com/finverse/shared/telemetry"
	"github.com/finverse/wallet-service/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	grpcClient ledgerv1.LedgerServiceClient
	conn       *grpc.ClientConn
}

// NewClient dials Transaction Service. Plaintext internal gRPC is the
// documented Phase 2 posture — mTLS is a Phase 4 AWS concern
// (docs/11-security-standards.md §7).
//
// conn.Connect() kicks off the connection handshake immediately rather than
// lazily on the first RPC (grpc.NewClient's default behavior) — see
// services/account-service/internal/client/ledger/client.go's comment on
// why: an un-warmed first call measurably timed out in local testing.
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

func toProtoDirection(d string) ledgerv1.Direction {
	if d == service.DirectionCredit {
		return ledgerv1.Direction_DIRECTION_CREDIT
	}
	return ledgerv1.Direction_DIRECTION_DEBIT
}

// PostTransaction implements service.LedgerClient, per docs/05-system-architecture.md §4
// (Wallet<->Transaction is a designated synchronous pair).
func (c *Client) PostTransaction(ctx context.Context, idempotencyKey string, legs []service.LedgerLeg) (string, error) {
	protoLegs := make([]*ledgerv1.Leg, len(legs))
	for i, l := range legs {
		protoLegs[i] = &ledgerv1.Leg{
			ParticipantId:   l.ParticipantID,
			ParticipantType: l.ParticipantType,
			AmountMinor:     l.AmountMinor,
			Direction:       toProtoDirection(l.Direction),
			Currency:        l.Currency,
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := c.grpcClient.PostTransaction(ctx, &ledgerv1.PostTransactionRequest{
		IdempotencyKey: idempotencyKey,
		SourceService:  service.SourceServiceName,
		Legs:           protoLegs,
	})
	if err != nil {
		return "", fmt.Errorf("calling PostTransaction: %w", err)
	}
	return resp.GetTransactionId(), nil
}
