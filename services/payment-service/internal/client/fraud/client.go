// Package fraud wraps the generated fraudv1 gRPC client so internal/service
// never imports it directly (docs/12-coding-standards.md §1.1).
package fraud

import (
	"context"
	"fmt"
	"github.com/finverse/shared/svcauth"

	"github.com/finverse/payment-service/internal/service"
	fraudv1 "github.com/finverse/shared/genproto/fraud/v1"
	"github.com/finverse/shared/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	grpcClient fraudv1.FraudServiceClient
	conn       *grpc.ClientConn
}

// NewClient dials Fraud Service. conn.Connect() warms the connection eagerly
// for the same reason the ledger client does: an un-warmed first call can
// blow the (here 300ms) call budget.
func NewClient(addr string, signer *svcauth.Signer) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithChainUnaryInterceptor(signer.UnaryClientInterceptor()),
		grpc.WithStatsHandler(telemetry.GRPCClientStats()))
	if err != nil {
		return nil, fmt.Errorf("dialing fraud-service at %s: %w", addr, err)
	}
	conn.Connect()
	return &Client{grpcClient: fraudv1.NewFraudServiceClient(conn), conn: conn}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) CheckTransaction(ctx context.Context, paymentID, fromAccountID, toReference string, amountMinor int64, rail, idempotencyKey string) (service.FraudVerdict, error) {
	resp, err := c.grpcClient.CheckTransaction(ctx, &fraudv1.CheckTransactionRequest{
		AccountId: fromAccountID, CounterpartyRef: toReference, AmountMinor: amountMinor,
		Rail: rail, IdempotencyKey: idempotencyKey, PaymentId: paymentID,
	})
	if err != nil {
		return service.FraudVerdict{}, fmt.Errorf("calling CheckTransaction: %w", err)
	}
	d := service.FraudAllow
	switch resp.GetVerdict() {
	case fraudv1.Verdict_VERDICT_REVIEW:
		d = service.FraudReview
	case fraudv1.Verdict_VERDICT_DENY:
		d = service.FraudDeny
	case fraudv1.Verdict_VERDICT_ALLOW:
	default:
		// An unspecified verdict is a contract violation, not an "allow".
		return service.FraudVerdict{}, fmt.Errorf("fraud-service returned unspecified verdict")
	}
	return service.FraudVerdict{Decision: d, RiskScore: int(resp.GetRiskScore()), Rule: resp.GetRule(), Reason: resp.GetReason()}, nil
}
