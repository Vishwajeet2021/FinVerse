package service

import (
	"context"
	"errors"
	"testing"

	"github.com/finverse/payment-service/internal/domain"
)

func newTestPaymentService() (*PaymentService, *fakePaymentRepository, *fakeLedgerClient) {
	repo := newFakePaymentRepository()
	ledger := &fakeLedgerClient{}
	return NewPaymentService(repo, ledger), repo, ledger
}

func TestInitiatePayment_SendsCorrectLegsAndCompletes(t *testing.T) {
	svc, _, ledger := newTestPaymentService()
	ctx := context.Background()

	payment, err := svc.InitiatePayment(ctx, "acct-1", "acct-2", 500_00, "idem-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payment.Status != domain.StatusCompleted {
		t.Errorf("got status %v, want COMPLETED", payment.Status)
	}
	if payment.TransactionID == nil {
		t.Error("expected a transaction ID to be set")
	}

	if len(ledger.calls) != 1 {
		t.Fatalf("expected exactly one PostTransaction call, got %d", len(ledger.calls))
	}
	legs := ledger.calls[0].Legs
	if len(legs) != 2 {
		t.Fatalf("expected 2 legs, got %d", len(legs))
	}
	if legs[0].ParticipantID != "acct-1" || legs[0].Direction != DirectionDebit {
		t.Errorf("leg 0 should DEBIT the source account, got %+v", legs[0])
	}
	if legs[1].ParticipantID != "acct-2" || legs[1].Direction != DirectionCredit {
		t.Errorf("leg 1 should CREDIT the destination account, got %+v", legs[1])
	}
	if ledger.calls[0].IdempotencyKey != "idem-1" {
		t.Errorf("idempotency key should propagate all the way to the ledger call, got %q", ledger.calls[0].IdempotencyKey)
	}
}

func TestInitiatePayment_IdempotentReplayDoesNotCallLedgerAgain(t *testing.T) {
	svc, _, ledger := newTestPaymentService()
	ctx := context.Background()

	first, err := svc.InitiatePayment(ctx, "acct-1", "acct-2", 500_00, "idem-replay")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	second, err := svc.InitiatePayment(ctx, "acct-1", "acct-2", 999_00, "idem-replay") // different amount, same key
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("replay returned a different payment: got %s, want %s", second.ID, first.ID)
	}
	if len(ledger.calls) != 1 {
		t.Fatalf("replay must not call the ledger a second time, got %d calls", len(ledger.calls))
	}
}

func TestInitiatePayment_LedgerFailureMarksPaymentFailed(t *testing.T) {
	svc, repo, ledger := newTestPaymentService()
	ctx := context.Background()
	ledger.err = errors.New("transaction-service unavailable")

	payment, err := svc.InitiatePayment(ctx, "acct-1", "acct-2", 500_00, "idem-1")
	if err == nil {
		t.Fatal("expected the ledger failure to propagate as an error")
	}
	if payment == nil || payment.Status != domain.StatusFailed {
		t.Fatalf("expected payment status FAILED, got %+v", payment)
	}
	if payment.FailureReason == nil {
		t.Error("expected a failure reason to be recorded")
	}

	// Confirm it's not stuck at INITIATED/PROCESSING in storage either.
	stored, getErr := repo.GetByID(ctx, payment.ID)
	if getErr != nil {
		t.Fatalf("reloading payment: %v", getErr)
	}
	if stored.Status != domain.StatusFailed {
		t.Fatalf("stored payment status = %v, want FAILED (must not be left stuck at PROCESSING)", stored.Status)
	}

	found := false
	for _, e := range repo.outboxEvents {
		if e.EventType == EventPaymentFailed {
			found = true
		}
	}
	if !found {
		t.Error("expected a PaymentFailed outbox event")
	}
}

func TestInitiatePayment_RejectsSameAccountTransfer(t *testing.T) {
	svc, _, _ := newTestPaymentService()
	if _, err := svc.InitiatePayment(context.Background(), "acct-1", "acct-1", 100_00, "idem-1"); err != domain.ErrSameAccount {
		t.Fatalf("got %v, want ErrSameAccount", err)
	}
}

func TestInitiatePayment_RejectsNonPositiveAmount(t *testing.T) {
	svc, _, _ := newTestPaymentService()
	ctx := context.Background()

	if _, err := svc.InitiatePayment(ctx, "acct-1", "acct-2", 0, "idem-1"); err != domain.ErrInvalidAmount {
		t.Fatalf("got %v, want ErrInvalidAmount", err)
	}
	if _, err := svc.InitiatePayment(ctx, "acct-1", "acct-2", -50, "idem-2"); err != domain.ErrInvalidAmount {
		t.Fatalf("got %v, want ErrInvalidAmount", err)
	}
}
