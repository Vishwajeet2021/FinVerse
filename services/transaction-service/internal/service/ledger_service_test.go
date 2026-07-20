package service

import (
	"context"
	"testing"

	"github.com/finverse/transaction-service/internal/domain"
)

func TestPostTransaction_Success(t *testing.T) {
	repo := newFakeTransactionRepository()
	svc := NewLedgerService(repo)
	ctx := context.Background()

	legs := []domain.Leg{
		newLeg("acct-1", 500_00, domain.DirectionDebit, "INR"),
		newLeg("acct-2", 500_00, domain.DirectionCredit, "INR"),
	}

	txn, err := svc.PostTransaction(ctx, "payment-service", "idem-1", legs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if txn.Status != domain.TransactionStatusPosted {
		t.Errorf("got status %v, want POSTED", txn.Status)
	}
	if len(repo.outboxEvents) != 1 || repo.outboxEvents[0].EventType != EventTransactionPosted {
		t.Errorf("expected exactly one TransactionPosted outbox event, got %+v", repo.outboxEvents)
	}
}

func TestPostTransaction_RejectsUnbalancedLegs(t *testing.T) {
	repo := newFakeTransactionRepository()
	svc := NewLedgerService(repo)
	ctx := context.Background()

	legs := []domain.Leg{
		newLeg("acct-1", 500_00, domain.DirectionDebit, "INR"),
		newLeg("acct-2", 499_00, domain.DirectionCredit, "INR"),
	}

	if _, err := svc.PostTransaction(ctx, "payment-service", "idem-2", legs); err != domain.ErrUnbalancedLegs {
		t.Fatalf("got %v, want ErrUnbalancedLegs", err)
	}
	if len(repo.byID) != 0 {
		t.Errorf("an invalid transaction must never be persisted, found %d", len(repo.byID))
	}
}

func TestPostTransaction_IdempotentReplayReturnsOriginal(t *testing.T) {
	repo := newFakeTransactionRepository()
	svc := NewLedgerService(repo)
	ctx := context.Background()

	legs := []domain.Leg{
		newLeg("acct-1", 100_00, domain.DirectionDebit, "INR"),
		newLeg("acct-2", 100_00, domain.DirectionCredit, "INR"),
	}

	first, err := svc.PostTransaction(ctx, "payment-service", "idem-replay", legs)
	if err != nil {
		t.Fatalf("first post: %v", err)
	}

	// A different (even malformed/unbalanced) leg set with the SAME
	// idempotency key must still return the original result — FR-TXN-3 says
	// "returns the original result without double-posting," not "validates
	// the replay's payload matches."
	differentLegs := []domain.Leg{newLeg("acct-3", 1, domain.DirectionDebit, "INR")}
	second, err := svc.PostTransaction(ctx, "payment-service", "idem-replay", differentLegs)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("replay returned a different transaction: got %s, want %s", second.ID, first.ID)
	}
	if len(repo.outboxEvents) != 1 {
		t.Errorf("replay must not post a second time, expected 1 outbox event, got %d", len(repo.outboxEvents))
	}
}

func TestReverseTransaction_PostsExactInverseAndMarksOriginal(t *testing.T) {
	repo := newFakeTransactionRepository()
	svc := NewLedgerService(repo)
	ctx := context.Background()

	original, err := svc.PostTransaction(ctx, "payment-service", "idem-orig", []domain.Leg{
		newLeg("acct-1", 250_00, domain.DirectionDebit, "INR"),
		newLeg("acct-2", 250_00, domain.DirectionCredit, "INR"),
	})
	if err != nil {
		t.Fatalf("posting original: %v", err)
	}

	reversal, err := svc.ReverseTransaction(ctx, "payment-service", original.ID, "idem-reverse")
	if err != nil {
		t.Fatalf("reversing: %v", err)
	}

	if reversal.ReversesTransactionID == nil || *reversal.ReversesTransactionID != original.ID {
		t.Fatalf("reversal does not reference the original transaction")
	}
	if err := reversal.Validate(); err != nil {
		t.Fatalf("reversal legs are not themselves balanced: %v", err)
	}

	// Every original leg's direction must be exactly inverted, amount unchanged.
	for _, orig := range original.Legs {
		found := false
		for _, rev := range reversal.Legs {
			if rev.ParticipantID == orig.ParticipantID && rev.AmountMinor == orig.AmountMinor {
				wantDir := domain.DirectionCredit
				if orig.Direction == domain.DirectionCredit {
					wantDir = domain.DirectionDebit
				}
				if rev.Direction != wantDir {
					t.Errorf("leg for %s: got direction %v, want %v", orig.ParticipantID, rev.Direction, wantDir)
				}
				found = true
			}
		}
		if !found {
			t.Errorf("no corresponding reversal leg found for original leg on %s", orig.ParticipantID)
		}
	}

	updatedOriginal, err := repo.GetByID(ctx, original.ID)
	if err != nil {
		t.Fatalf("reloading original: %v", err)
	}
	if updatedOriginal.Status != domain.TransactionStatusReversed {
		t.Errorf("original transaction status = %v, want REVERSED", updatedOriginal.Status)
	}

	// The original ledger row itself must still exist, unmodified in its
	// legs — a reversal is a new transaction, never a delete or edit
	// (docs/06-domain-driven-design.md §2.5, FR-TXN-4).
	if len(updatedOriginal.Legs) != 2 {
		t.Errorf("original transaction's legs were altered: got %d legs, want 2", len(updatedOriginal.Legs))
	}
}

func TestReverseTransaction_RejectsDoubleReversal(t *testing.T) {
	repo := newFakeTransactionRepository()
	svc := NewLedgerService(repo)
	ctx := context.Background()

	original, err := svc.PostTransaction(ctx, "payment-service", "idem-orig2", []domain.Leg{
		newLeg("acct-1", 100_00, domain.DirectionDebit, "INR"),
		newLeg("acct-2", 100_00, domain.DirectionCredit, "INR"),
	})
	if err != nil {
		t.Fatalf("posting original: %v", err)
	}

	if _, err := svc.ReverseTransaction(ctx, "payment-service", original.ID, "idem-rev-a"); err != nil {
		t.Fatalf("first reversal: %v", err)
	}
	if _, err := svc.ReverseTransaction(ctx, "payment-service", original.ID, "idem-rev-b"); err != domain.ErrAlreadyReversed {
		t.Fatalf("got %v, want ErrAlreadyReversed", err)
	}
}
