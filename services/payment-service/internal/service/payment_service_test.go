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
	return NewPaymentService(repo, ledger, newFakeAccountResolver(), &fakeFraudChecker{}, false), repo, ledger
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

func TestInitiatePayment_RejectsUnknownAccountNumberBeforeCallingLedger(t *testing.T) {
	repo := newFakePaymentRepository()
	ledger := &fakeLedgerClient{}
	resolver := newFakeAccountResolver()
	resolver.missing["999999999999"] = true
	svc := NewPaymentService(repo, ledger, resolver, &fakeFraudChecker{}, false)

	_, err := svc.InitiatePayment(context.Background(), "acct-1", "999999999999", 100_00, "idem-1")
	if err != domain.ErrRecipientAccountNotFound {
		t.Fatalf("got %v, want ErrRecipientAccountNotFound", err)
	}
	if len(ledger.calls) != 0 {
		t.Errorf("an unresolvable recipient must never reach the ledger, got %d calls", len(ledger.calls))
	}
	if len(repo.byID) != 0 {
		t.Errorf("an unresolvable recipient must not create a payment record, got %d", len(repo.byID))
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

func newFraudTestService(f *fakeFraudChecker, failOpen bool) (*PaymentService, *fakePaymentRepository, *fakeLedgerClient) {
	repo := newFakePaymentRepository()
	ledger := &fakeLedgerClient{}
	return NewPaymentService(repo, ledger, newFakeAccountResolver(), f, failOpen), repo, ledger
}

func TestInitiatePayment_FraudDenyFailsPaymentAndNeverTouchesLedger(t *testing.T) {
	fraud := &fakeFraudChecker{verdict: FraudVerdict{Decision: FraudDeny, Rule: "VELOCITY", Reason: "Too many transfers."}}
	svc, repo, ledger := newFraudTestService(fraud, false)

	p, err := svc.InitiatePayment(context.Background(), "acct-1", "acct-2", 100, "idem-f1")
	if err != nil || p == nil {
		t.Fatalf("a denied payment is a FAILED payment record, not an error: p=%v err=%v", p, err)
	}
	if p.Status != domain.StatusFailed || p.FailureReason == nil || *p.FailureReason != "FRAUD_DENIED: Too many transfers." {
		t.Errorf("got status=%v reason=%v", p.Status, p.FailureReason)
	}
	if len(ledger.calls) != 0 {
		t.Errorf("ledger called %d times for a denied payment", len(ledger.calls))
	}
	last := repo.outboxEvents[len(repo.outboxEvents)-1]
	if last.EventType != EventPaymentFailed {
		t.Errorf("last event = %s, want PaymentFailed", last.EventType)
	}
}

func TestInitiatePayment_FraudReviewStillCompletes(t *testing.T) {
	svc, _, ledger := newFraudTestService(&fakeFraudChecker{verdict: FraudVerdict{Decision: FraudReview}}, false)
	p, err := svc.InitiatePayment(context.Background(), "acct-1", "acct-2", 100, "idem-f2")
	if err != nil || p.Status != domain.StatusCompleted || len(ledger.calls) != 1 {
		t.Fatalf("REVIEW must proceed: p=%+v err=%v ledgerCalls=%d", p, err, len(ledger.calls))
	}
}

func TestInitiatePayment_FraudUnavailableFailsClosedByDefault(t *testing.T) {
	svc, _, ledger := newFraudTestService(&fakeFraudChecker{err: errors.New("connection refused")}, false)
	p, err := svc.InitiatePayment(context.Background(), "acct-1", "acct-2", 100, "idem-f3")
	if err == nil || p == nil || p.Status != domain.StatusFailed {
		t.Fatalf("want a FAILED payment plus an error, got p=%+v err=%v", p, err)
	}
	if len(ledger.calls) != 0 {
		t.Error("money moved without a fraud verdict")
	}
}

func TestInitiatePayment_FraudUnavailableFailOpenProceeds(t *testing.T) {
	svc, _, ledger := newFraudTestService(&fakeFraudChecker{err: errors.New("timeout")}, true)
	p, err := svc.InitiatePayment(context.Background(), "acct-1", "acct-2", 100, "idem-f4")
	if err != nil || p.Status != domain.StatusCompleted || len(ledger.calls) != 1 {
		t.Fatalf("fail-open must proceed: p=%+v err=%v", p, err)
	}
}
