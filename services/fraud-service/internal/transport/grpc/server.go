// Package grpc exposes CheckTransaction to Payment Service
// (docs/05-system-architecture.md §4).
package grpc

import (
	"context"
	"time"

	"github.com/finverse/fraud-service/internal/rules"
	"github.com/finverse/fraud-service/internal/service"
	fraudv1 "github.com/finverse/shared/genproto/fraud/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	fraudv1.UnimplementedFraudServiceServer
	svc *service.Service
}

func NewServer(svc *service.Service) *Server { return &Server{svc: svc} }

func (s *Server) CheckTransaction(ctx context.Context, req *fraudv1.CheckTransactionRequest) (*fraudv1.CheckTransactionResponse, error) {
	if req.GetAccountId() == "" || req.GetAmountMinor() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "account_id and a positive amount_minor are required")
	}
	start := time.Now()
	d, err := s.svc.Check(ctx, service.Request{
		AccountID: req.GetAccountId(), CounterpartyRef: req.GetCounterpartyRef(), AmountMinor: req.GetAmountMinor(),
		Rail: req.GetRail(), IdempotencyKey: req.GetIdempotencyKey(), PaymentID: req.GetPaymentId(),
	})
	checkDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		checksTotal.WithLabelValues("ERROR").Inc()
		// Verdict unknown: tell the caller so it applies its own policy.
		return nil, status.Error(codes.Unavailable, "risk evaluation unavailable")
	}
	checksTotal.WithLabelValues(string(d.Verdict)).Inc()
	v := fraudv1.Verdict_VERDICT_ALLOW
	switch d.Verdict {
	case rules.Review:
		v = fraudv1.Verdict_VERDICT_REVIEW
	case rules.Deny:
		v = fraudv1.Verdict_VERDICT_DENY
	}
	return &fraudv1.CheckTransactionResponse{Verdict: v, RiskScore: int32(d.RiskScore), Rule: d.Rule, Reason: d.Reason}, nil
}
