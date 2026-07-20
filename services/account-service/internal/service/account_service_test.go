package service

import (
	"context"
	"testing"

	"github.com/finverse/account-service/internal/domain"
)

func newTestAccountService() (*AccountService, *fakeAccountRepository) {
	repo := newFakeAccountRepository()
	return NewAccountService(repo, &fakeLedgerHistoryClient{}), repo
}

func TestOpenAccount_Success(t *testing.T) {
	svc, repo := newTestAccountService()
	ctx := context.Background()

	account, err := svc.OpenAccount(ctx, "customer-1", "SAVINGS")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if account.Status != domain.StatusActive {
		t.Errorf("got status %v, want ACTIVE", account.Status)
	}
	if len(repo.outboxEvents) != 1 || repo.outboxEvents[0].EventType != EventAccountOpened {
		t.Errorf("expected exactly one AccountOpened outbox event, got %+v", repo.outboxEvents)
	}

	isHolder, err := svc.IsAccountHolder(ctx, account.ID, "customer-1")
	if err != nil || !isHolder {
		t.Errorf("opening customer should be a holder: isHolder=%v err=%v", isHolder, err)
	}
}

func TestOpenAccount_RejectsInvalidProductType(t *testing.T) {
	svc, _ := newTestAccountService()
	if _, err := svc.OpenAccount(context.Background(), "customer-1", "CRYPTO"); err != domain.ErrInvalidProductType {
		t.Fatalf("got %v, want ErrInvalidProductType", err)
	}
}

func TestOpenAccount_RetriesOnAccountNumberCollision(t *testing.T) {
	svc, repo := newTestAccountService()
	repo.forceCollision = true // first attempt collides, OpenAccount must retry with a fresh number

	account, err := svc.OpenAccount(context.Background(), "customer-1", "SAVINGS")
	if err != nil {
		t.Fatalf("expected retry to succeed, got error: %v", err)
	}
	if account == nil {
		t.Fatal("expected a valid account after retry")
	}
}

func TestFreezeUnfreeze(t *testing.T) {
	svc, repo := newTestAccountService()
	ctx := context.Background()

	account, err := svc.OpenAccount(ctx, "customer-1", "SAVINGS")
	if err != nil {
		t.Fatalf("opening account: %v", err)
	}

	if err := svc.Freeze(ctx, account.ID, "suspicious activity"); err != nil {
		t.Fatalf("freezing: %v", err)
	}
	frozen, _ := repo.GetByID(ctx, account.ID)
	if frozen.Status != domain.StatusFrozen {
		t.Fatalf("got status %v, want FROZEN", frozen.Status)
	}

	if err := svc.Freeze(ctx, account.ID, "again"); err != domain.ErrAccountAlreadyFrozen {
		t.Fatalf("got %v, want ErrAccountAlreadyFrozen", err)
	}

	if err := svc.Unfreeze(ctx, account.ID); err != nil {
		t.Fatalf("unfreezing: %v", err)
	}
	active, _ := repo.GetByID(ctx, account.ID)
	if active.Status != domain.StatusActive {
		t.Fatalf("got status %v, want ACTIVE", active.Status)
	}

	if err := svc.Unfreeze(ctx, account.ID); err != domain.ErrAccountNotFrozen {
		t.Fatalf("got %v, want ErrAccountNotFrozen", err)
	}
}

// TestConsumeTransactionPosted_IdempotentOnRedelivery is the correctness
// property that matters most in this service: Redis Streams consumer groups
// are at-least-once delivery (shared/go/eventbus), so the same
// TransactionPosted event can be redelivered — applying its balance delta
// twice would silently corrupt a real balance. This proves it doesn't.
func TestConsumeTransactionPosted_IdempotentOnRedelivery(t *testing.T) {
	svc, _ := newTestAccountService()
	ctx := context.Background()

	account, err := svc.OpenAccount(ctx, "customer-1", "SAVINGS")
	if err != nil {
		t.Fatalf("opening account: %v", err)
	}

	payload := TransactionPostedPayload{
		TransactionID: "txn-1",
		SourceService: "payment-service",
		Legs: []legPayload{
			{ParticipantID: account.ID, ParticipantType: "ACCOUNT", AmountMinor: 500_00, Direction: "C", Currency: "INR"},
		},
	}

	const eventID = "event-1"
	if err := svc.ConsumeTransactionPosted(ctx, eventID, payload); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	firstBalance, err := svc.GetBalance(ctx, account.ID)
	if err != nil {
		t.Fatalf("getting balance: %v", err)
	}
	if firstBalance.LedgerBalanceMinor != 500_00 {
		t.Fatalf("got balance %d, want 50000", firstBalance.LedgerBalanceMinor)
	}

	// Redeliver the SAME event ID — must be a no-op.
	if err := svc.ConsumeTransactionPosted(ctx, eventID, payload); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	secondBalance, err := svc.GetBalance(ctx, account.ID)
	if err != nil {
		t.Fatalf("getting balance after redelivery: %v", err)
	}
	if secondBalance.LedgerBalanceMinor != 500_00 {
		t.Fatalf("balance changed on redelivery: got %d, want unchanged at 50000 (double-counted!)", secondBalance.LedgerBalanceMinor)
	}

	// A genuinely NEW event must still apply normally.
	payload2 := payload
	payload2.TransactionID = "txn-2"
	if err := svc.ConsumeTransactionPosted(ctx, "event-2", payload2); err != nil {
		t.Fatalf("second distinct event: %v", err)
	}
	thirdBalance, _ := svc.GetBalance(ctx, account.ID)
	if thirdBalance.LedgerBalanceMinor != 1000_00 {
		t.Fatalf("got balance %d, want 100000 after a second distinct credit", thirdBalance.LedgerBalanceMinor)
	}
}

func TestConsumeTransactionPosted_IgnoresLegsForOtherParticipantTypes(t *testing.T) {
	svc, _ := newTestAccountService()
	ctx := context.Background()

	account, err := svc.OpenAccount(ctx, "customer-1", "SAVINGS")
	if err != nil {
		t.Fatalf("opening account: %v", err)
	}

	payload := TransactionPostedPayload{
		TransactionID: "txn-1",
		SourceService: "wallet-service",
		Legs: []legPayload{
			{ParticipantID: "some-wallet-id", ParticipantType: "WALLET", AmountMinor: 100_00, Direction: "C", Currency: "INR"},
		},
	}

	if err := svc.ConsumeTransactionPosted(ctx, "event-1", payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	balance, err := svc.GetBalance(ctx, account.ID)
	if err != nil {
		t.Fatalf("getting balance: %v", err)
	}
	if balance.LedgerBalanceMinor != 0 {
		t.Fatalf("a WALLET-type leg should never affect an ACCOUNT balance, got %d", balance.LedgerBalanceMinor)
	}
}
