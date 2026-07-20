package service

import (
	"context"
	"time"

	"github.com/finverse/wallet-service/internal/domain"
	"github.com/google/uuid"
)

// In-memory fakes for WalletRepository/LedgerClient, mirroring
// transaction-service's and account-service's fakes_test.go pattern --
// per docs/17-testing-strategy.md §2, business logic is verified without a
// real database or a real gRPC connection.

type outboxEntry struct {
	EventType string
	Payload   any
}

type fakeWalletRepository struct {
	byID            map[string]*domain.Wallet
	byCustomerID    map[string]*domain.Wallet
	balances        map[string]*domain.BalanceProjection
	processedEvents map[string]bool
	outboxEvents    []outboxEntry
}

func newFakeWalletRepository() *fakeWalletRepository {
	return &fakeWalletRepository{
		byID:            map[string]*domain.Wallet{},
		byCustomerID:    map[string]*domain.Wallet{},
		balances:        map[string]*domain.BalanceProjection{},
		processedEvents: map[string]bool{},
	}
}

func (f *fakeWalletRepository) CreateWalletWithProjection(_ context.Context, wallet *domain.Wallet, eventType string, payload any) error {
	if _, exists := f.byCustomerID[wallet.CustomerID]; exists {
		return domain.ErrWalletAlreadyExists
	}
	cp := *wallet
	f.byID[wallet.ID] = &cp
	f.byCustomerID[wallet.CustomerID] = &cp
	f.balances[wallet.ID] = &domain.BalanceProjection{WalletID: wallet.ID, UpdatedAt: time.Now()}
	f.outboxEvents = append(f.outboxEvents, outboxEntry{EventType: eventType, Payload: payload})
	return nil
}

func (f *fakeWalletRepository) GetByID(_ context.Context, id string) (*domain.Wallet, error) {
	w, ok := f.byID[id]
	if !ok {
		return nil, domain.ErrWalletNotFound
	}
	cp := *w
	return &cp, nil
}

func (f *fakeWalletRepository) GetByCustomerID(_ context.Context, customerID string) (*domain.Wallet, error) {
	w, ok := f.byCustomerID[customerID]
	if !ok {
		return nil, domain.ErrWalletNotFound
	}
	cp := *w
	return &cp, nil
}

func (f *fakeWalletRepository) ExistsByID(_ context.Context, id string) (bool, error) {
	_, ok := f.byID[id]
	return ok, nil
}

func (f *fakeWalletRepository) GetBalance(_ context.Context, walletID string) (*domain.BalanceProjection, error) {
	b, ok := f.balances[walletID]
	if !ok {
		return nil, domain.ErrWalletNotFound
	}
	cp := *b
	return &cp, nil
}

func (f *fakeWalletRepository) ApplyTransactionPostedIdempotent(_ context.Context, eventID, transactionID string, deltas []WalletDelta) (bool, error) {
	if f.processedEvents[eventID] {
		return false, nil
	}
	f.processedEvents[eventID] = true

	for _, d := range deltas {
		b, ok := f.balances[d.WalletID]
		if !ok {
			continue
		}
		b.BalanceMinor += d.DeltaMinor
		txnID := transactionID
		b.LastTransactionID = &txnID
		b.UpdatedAt = time.Now()
	}
	return true, nil
}

func (f *fakeWalletRepository) InsertOutboxEvent(_ context.Context, eventType string, payload any) error {
	f.outboxEvents = append(f.outboxEvents, outboxEntry{EventType: eventType, Payload: payload})
	return nil
}

// fakeLedgerClient records every PostTransaction call so tests can assert on
// the exact legs sent, and lets tests inject a failure.
type fakeLedgerClient struct {
	calls []postTransactionCall
	err   error
}

type postTransactionCall struct {
	IdempotencyKey string
	Legs           []LedgerLeg
}

func (f *fakeLedgerClient) PostTransaction(_ context.Context, idempotencyKey string, legs []LedgerLeg) (string, error) {
	f.calls = append(f.calls, postTransactionCall{IdempotencyKey: idempotencyKey, Legs: legs})
	if f.err != nil {
		return "", f.err
	}
	return uuid.NewString(), nil
}
