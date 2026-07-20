// Package grpc implements Transaction Service's internal gRPC surface —
// the sync call path used by Payment/Wallet Service, per
// docs/09-api-standards.md §3 and docs/05-system-architecture.md §4. This is
// never exposed via the public API Gateway.
package grpc

import (
	"context"
	"time"

	"github.com/finverse/transaction-service/internal/domain"
	"github.com/finverse/transaction-service/internal/service"
	ledgerv1 "github.com/finverse/shared/genproto/ledger/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	ledgerv1.UnimplementedLedgerServiceServer
	ledger *service.LedgerService
}

func NewServer(ledger *service.LedgerService) *Server {
	return &Server{ledger: ledger}
}

func toDomainDirection(d ledgerv1.Direction) domain.Direction {
	if d == ledgerv1.Direction_DIRECTION_CREDIT {
		return domain.DirectionCredit
	}
	return domain.DirectionDebit
}

func toProtoDirection(d domain.Direction) ledgerv1.Direction {
	if d == domain.DirectionCredit {
		return ledgerv1.Direction_DIRECTION_CREDIT
	}
	return ledgerv1.Direction_DIRECTION_DEBIT
}

func (s *Server) PostTransaction(ctx context.Context, req *ledgerv1.PostTransactionRequest) (*ledgerv1.PostTransactionResponse, error) {
	if req.GetIdempotencyKey() == "" || req.GetSourceService() == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key and source_service are required")
	}

	legs := make([]domain.Leg, len(req.GetLegs()))
	for i, l := range req.GetLegs() {
		legs[i] = domain.Leg{
			ParticipantID:   l.GetParticipantId(),
			ParticipantType: domain.ParticipantType(l.GetParticipantType()),
			AmountMinor:     l.GetAmountMinor(),
			Direction:       toDomainDirection(l.GetDirection()),
			Currency:        l.GetCurrency(),
		}
	}

	txn, err := s.ledger.PostTransaction(ctx, req.GetSourceService(), req.GetIdempotencyKey(), legs)
	if err != nil {
		return nil, mapDomainError(err)
	}

	return &ledgerv1.PostTransactionResponse{
		TransactionId: txn.ID,
		Status:        ledgerv1.TransactionStatus_TRANSACTION_STATUS_POSTED,
	}, nil
}

func (s *Server) ReverseTransaction(ctx context.Context, req *ledgerv1.ReverseTransactionRequest) (*ledgerv1.ReverseTransactionResponse, error) {
	if req.GetTransactionId() == "" || req.GetIdempotencyKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "transaction_id and idempotency_key are required")
	}

	reversal, err := s.ledger.ReverseTransaction(ctx, "transaction-service-reversal", req.GetTransactionId(), req.GetIdempotencyKey())
	if err != nil {
		return nil, mapDomainError(err)
	}

	return &ledgerv1.ReverseTransactionResponse{ReversalTransactionId: reversal.ID}, nil
}

func (s *Server) GetAccountHistory(ctx context.Context, req *ledgerv1.GetAccountHistoryRequest) (*ledgerv1.GetAccountHistoryResponse, error) {
	if req.GetParticipantId() == "" {
		return nil, status.Error(codes.InvalidArgument, "participant_id is required")
	}

	var from, to *time.Time
	if req.GetFrom() != "" {
		t, err := time.Parse(time.RFC3339, req.GetFrom())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "from must be RFC3339")
		}
		from = &t
	}
	if req.GetTo() != "" {
		t, err := time.Parse(time.RFC3339, req.GetTo())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "to must be RFC3339")
		}
		to = &t
	}

	legs, nextCursor, err := s.ledger.GetAccountHistory(ctx, req.GetParticipantId(), from, to, int(req.GetLimit()), req.GetCursor())
	if err != nil {
		return nil, mapDomainError(err)
	}

	entries := make([]*ledgerv1.HistoryEntry, len(legs))
	for i, l := range legs {
		entries[i] = &ledgerv1.HistoryEntry{
			TransactionId: l.TransactionID,
			LegId:         l.ID,
			AmountMinor:   l.AmountMinor,
			Direction:     toProtoDirection(l.Direction),
			Currency:      l.Currency,
		}
	}

	return &ledgerv1.GetAccountHistoryResponse{Entries: entries, NextCursor: nextCursor}, nil
}

func mapDomainError(err error) error {
	switch err {
	case domain.ErrUnbalancedLegs, domain.ErrTooFewLegs, domain.ErrNonPositiveAmount, domain.ErrInvalidDirection:
		return status.Error(codes.InvalidArgument, err.Error())
	case domain.ErrTransactionNotFound:
		return status.Error(codes.NotFound, err.Error())
	case domain.ErrAlreadyReversed:
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.Internal, "internal error posting to the ledger")
	}
}
