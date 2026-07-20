package service

import (
	"context"
	"errors"
	"testing"

	"github.com/finverse/wallet-service/internal/domain"
)

func newTestWalletService() (*WalletService, *fakeWalletRepository, *fakeLedgerClient) {
	repo := newFakeWalletRepository()
	ledger := &fakeLedgerClient{}
	return NewWalletService(repo, ledger), repo, ledger
}

func TestProvisionWallet_OnePerCustomer(t *testing.T) {
	svc, _, _ := newTestWalletService()
	ctx := context.Background()

	wallet, err := svc.ProvisionWallet(ctx, "customer-1", "account-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wallet.Status != domain.WalletStatusActive {
		t.Errorf("got status %v, want ACTIVE", wallet.Status)
	}

	if _, err := svc.ProvisionWallet(ctx, "customer-1", "account-1"); err != domain.ErrWalletAlreadyExists {
		t.Fatalf("got %v, want ErrWalletAlreadyExists", err)
	}
}

func TestTopUp_SendsCorrectLegs(t *testing.T) {
	svc, _, ledger := newTestWalletService()
	ctx := context.Background()

	wallet, err := svc.ProvisionWallet(ctx, "customer-1", "account-1")
	if err != nil {
		t.Fatalf("provisioning: %v", err)
	}

	if err := svc.TopUp(ctx, wallet.ID, 500_00, "idem-topup-1"); err != nil {
		t.Fatalf("top-up: %v", err)
	}

	if len(ledger.calls) != 1 {
		t.Fatalf("expected exactly one PostTransaction call, got %d", len(ledger.calls))
	}
	legs := ledger.calls[0].Legs
	if len(legs) != 2 {
		t.Fatalf("expected 2 legs, got %d", len(legs))
	}
	if legs[0].ParticipantID != "account-1" || legs[0].ParticipantType != ParticipantAccount || legs[0].Direction != DirectionDebit {
		t.Errorf("leg 0 should DEBIT the funding account, got %+v", legs[0])
	}
	if legs[1].ParticipantID != wallet.ID || legs[1].ParticipantType != ParticipantWallet || legs[1].Direction != DirectionCredit {
		t.Errorf("leg 1 should CREDIT the wallet, got %+v", legs[1])
	}
	if legs[0].AmountMinor != 500_00 || legs[1].AmountMinor != 500_00 {
		t.Errorf("both legs should carry the same amount, got %+v", legs)
	}
}

func TestTopUp_RejectsNonPositiveAmount(t *testing.T) {
	svc, _, _ := newTestWalletService()
	ctx := context.Background()
	wallet, _ := svc.ProvisionWallet(ctx, "customer-1", "account-1")

	if err := svc.TopUp(ctx, wallet.ID, 0, "idem-1"); err != domain.ErrInvalidAmount {
		t.Fatalf("got %v, want ErrInvalidAmount", err)
	}
	if err := svc.TopUp(ctx, wallet.ID, -100, "idem-2"); err != domain.ErrInvalidAmount {
		t.Fatalf("got %v, want ErrInvalidAmount", err)
	}
}

func TestTransfer_RejectsSameWallet(t *testing.T) {
	svc, _, _ := newTestWalletService()
	ctx := context.Background()
	wallet, _ := svc.ProvisionWallet(ctx, "customer-1", "account-1")

	if err := svc.Transfer(ctx, wallet.ID, wallet.ID, 100_00, "idem-1"); err != domain.ErrSameWallet {
		t.Fatalf("got %v, want ErrSameWallet", err)
	}
}

func TestTransfer_RejectsInsufficientBalance(t *testing.T) {
	svc, repo, _ := newTestWalletService()
	ctx := context.Background()

	from, _ := svc.ProvisionWallet(ctx, "customer-1", "account-1")
	to, _ := svc.ProvisionWallet(ctx, "customer-2", "account-2")
	_ = repo // balance starts at zero, no top-up performed

	if err := svc.Transfer(ctx, from.ID, to.ID, 1, "idem-1"); err != domain.ErrInsufficientBalance {
		t.Fatalf("got %v, want ErrInsufficientBalance", err)
	}
}

func TestTransfer_SendsCorrectLegsAndSucceedsWhenFunded(t *testing.T) {
	svc, repo, ledger := newTestWalletService()
	ctx := context.Background()

	from, _ := svc.ProvisionWallet(ctx, "customer-1", "account-1")
	to, _ := svc.ProvisionWallet(ctx, "customer-2", "account-2")

	// Fund the sender directly in the fake balance store (simulating a prior
	// top-up's event having already been consumed).
	repo.balances[from.ID].BalanceMinor = 1000_00

	if err := svc.Transfer(ctx, from.ID, to.ID, 300_00, "idem-transfer-1"); err != nil {
		t.Fatalf("transfer: %v", err)
	}

	if len(ledger.calls) != 1 {
		t.Fatalf("expected exactly one PostTransaction call, got %d", len(ledger.calls))
	}
	legs := ledger.calls[0].Legs
	if legs[0].ParticipantID != from.ID || legs[0].Direction != DirectionDebit {
		t.Errorf("leg 0 should DEBIT the sending wallet, got %+v", legs[0])
	}
	if legs[1].ParticipantID != to.ID || legs[1].Direction != DirectionCredit {
		t.Errorf("leg 1 should CREDIT the receiving wallet, got %+v", legs[1])
	}
}

func TestTransfer_LedgerFailurePropagates(t *testing.T) {
	svc, repo, ledger := newTestWalletService()
	ctx := context.Background()

	from, _ := svc.ProvisionWallet(ctx, "customer-1", "account-1")
	to, _ := svc.ProvisionWallet(ctx, "customer-2", "account-2")
	repo.balances[from.ID].BalanceMinor = 1000_00
	ledger.err = errors.New("transaction-service unavailable")

	if err := svc.Transfer(ctx, from.ID, to.ID, 100_00, "idem-1"); err == nil {
		t.Fatal("expected the ledger failure to propagate")
	}
}

// TestConsumeTransactionPosted_IdempotentOnRedelivery proves the same
// correctness property account-service's equivalent test proves: a
// redelivered TransactionPosted event (Redis Streams consumer groups are
// at-least-once) must never double-count a wallet balance.
func TestConsumeTransactionPosted_IdempotentOnRedelivery(t *testing.T) {
	svc, _, _ := newTestWalletService()
	ctx := context.Background()

	wallet, _ := svc.ProvisionWallet(ctx, "customer-1", "account-1")

	payload := TransactionPostedPayload{
		TransactionID: "txn-1",
		SourceService: "wallet-service",
		Legs: []LegPayload{
			{ParticipantID: wallet.ID, ParticipantType: "WALLET", AmountMinor: 500_00, Direction: "C", Currency: "INR"},
		},
	}

	if err := svc.ConsumeTransactionPosted(ctx, "event-1", payload); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	balance, _ := svc.GetBalance(ctx, wallet.ID)
	if balance.BalanceMinor != 500_00 {
		t.Fatalf("got balance %d, want 50000", balance.BalanceMinor)
	}

	if err := svc.ConsumeTransactionPosted(ctx, "event-1", payload); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	balance, _ = svc.GetBalance(ctx, wallet.ID)
	if balance.BalanceMinor != 500_00 {
		t.Fatalf("balance changed on redelivery: got %d, want unchanged at 50000 (double-counted!)", balance.BalanceMinor)
	}
}

// TestConsumeTransactionPosted_AppliesBothLegsOfAWalletToWalletTransfer
// covers the subtlety unique to this service: a single wallet-to-wallet
// TransactionPosted event carries TWO legs this service owns (sender debit,
// receiver credit), both of which must apply from one event delivery.
func TestConsumeTransactionPosted_AppliesBothLegsOfAWalletToWalletTransfer(t *testing.T) {
	svc, repo, _ := newTestWalletService()
	ctx := context.Background()

	from, _ := svc.ProvisionWallet(ctx, "customer-1", "account-1")
	to, _ := svc.ProvisionWallet(ctx, "customer-2", "account-2")
	repo.balances[from.ID].BalanceMinor = 1000_00

	payload := TransactionPostedPayload{
		TransactionID: "txn-1",
		SourceService: "wallet-service",
		Legs: []LegPayload{
			{ParticipantID: from.ID, ParticipantType: "WALLET", AmountMinor: 300_00, Direction: "D", Currency: "INR"},
			{ParticipantID: to.ID, ParticipantType: "WALLET", AmountMinor: 300_00, Direction: "C", Currency: "INR"},
		},
	}

	if err := svc.ConsumeTransactionPosted(ctx, "event-1", payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fromBalance, _ := svc.GetBalance(ctx, from.ID)
	if fromBalance.BalanceMinor != 700_00 {
		t.Errorf("sender balance: got %d, want 70000", fromBalance.BalanceMinor)
	}
	toBalance, _ := svc.GetBalance(ctx, to.ID)
	if toBalance.BalanceMinor != 300_00 {
		t.Errorf("receiver balance: got %d, want 30000", toBalance.BalanceMinor)
	}
}
