package service

import (
	"context"
	"time"

	"github.com/finverse/account-service/internal/domain"
)

// In-memory fakes for AccountRepository/LedgerHistoryClient, used by this
// package's unit tests so business logic is verified without a real
// database — per docs/17-testing-strategy.md §2, mirroring Transaction
// Service's internal/service/fakes_test.go pattern. The database-level
// behavior (real UNIQUE constraints, real transactional atomicity) is a
// separate concern belonging to a Postgres-backed integration test, not
// this package.
type outboxEntry struct {
	EventType string
	Payload   any
}

type fakeAccountRepository struct {
	accounts        map[string]*domain.Account
	holders         map[string]map[string]*domain.AccountHolder // accountID -> customerID -> holder
	balances        map[string]*domain.BalanceProjection
	accountNumbers  map[string]bool
	processedEvents map[string]bool
	outboxEvents    []outboxEntry
	forceCollision  bool // simulates domain.ErrAccountNumberCollision on the next OpenAccountWithOutbox call
}

func newFakeAccountRepository() *fakeAccountRepository {
	return &fakeAccountRepository{
		accounts:        map[string]*domain.Account{},
		holders:         map[string]map[string]*domain.AccountHolder{},
		balances:        map[string]*domain.BalanceProjection{},
		accountNumbers:  map[string]bool{},
		processedEvents: map[string]bool{},
	}
}

func (f *fakeAccountRepository) OpenAccountWithOutbox(_ context.Context, account *domain.Account, holder *domain.AccountHolder, eventType string, payload any) error {
	if f.forceCollision {
		f.forceCollision = false
		return domain.ErrAccountNumberCollision
	}
	if f.accountNumbers[account.AccountNumber] {
		return domain.ErrAccountNumberCollision
	}

	accCopy := *account
	f.accounts[account.ID] = &accCopy
	f.accountNumbers[account.AccountNumber] = true

	holderCopy := *holder
	f.holders[account.ID] = map[string]*domain.AccountHolder{holder.CustomerID: &holderCopy}

	f.balances[account.ID] = &domain.BalanceProjection{AccountID: account.ID, UpdatedAt: time.Now()}
	f.outboxEvents = append(f.outboxEvents, outboxEntry{EventType: eventType, Payload: payload})
	return nil
}

func (f *fakeAccountRepository) GetByID(_ context.Context, accountID string) (*domain.Account, error) {
	a, ok := f.accounts[accountID]
	if !ok {
		return nil, domain.ErrAccountNotFound
	}
	cp := *a
	return &cp, nil
}

func (f *fakeAccountRepository) GetByAccountNumber(_ context.Context, accountNumber string) (*domain.Account, error) {
	for _, a := range f.accounts {
		if a.AccountNumber == accountNumber {
			cp := *a
			return &cp, nil
		}
	}
	return nil, domain.ErrAccountNotFound
}

func (f *fakeAccountRepository) ListByCustomerID(_ context.Context, customerID string) ([]domain.Account, error) {
	var out []domain.Account
	for _, a := range f.accounts {
		if a.CustomerID == customerID {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (f *fakeAccountRepository) GetHolder(_ context.Context, accountID, customerID string) (*domain.AccountHolder, error) {
	holders, ok := f.holders[accountID]
	if !ok {
		return nil, domain.ErrNotAccountHolder
	}
	h, ok := holders[customerID]
	if !ok {
		return nil, domain.ErrNotAccountHolder
	}
	cp := *h
	return &cp, nil
}

func (f *fakeAccountRepository) GetBalance(_ context.Context, accountID string) (*domain.BalanceProjection, error) {
	b, ok := f.balances[accountID]
	if !ok {
		return nil, domain.ErrAccountNotFound
	}
	cp := *b
	return &cp, nil
}

func (f *fakeAccountRepository) UpdateStatusWithOutbox(_ context.Context, accountID string, status domain.AccountStatus, frozenBy domain.Actor, eventType string, payload any) error {
	a, ok := f.accounts[accountID]
	if !ok {
		return domain.ErrAccountNotFound
	}
	a.Status = status
	a.FrozenBy = ""
	if status == domain.StatusFrozen {
		a.FrozenBy = frozenBy
	}
	f.outboxEvents = append(f.outboxEvents, outboxEntry{EventType: eventType, Payload: payload})
	return nil
}

func (f *fakeAccountRepository) ApplyTransactionPosted(_ context.Context, eventID, transactionID string, legs []LegUpdate) error {
	if f.processedEvents[eventID] {
		return nil // already processed — the redelivery no-op this whole mechanism exists for
	}
	f.processedEvents[eventID] = true

	for _, leg := range legs {
		b, ok := f.balances[leg.AccountID]
		if !ok {
			continue // not one of this service's own accounts — silent no-op, matching the real UPDATE's behavior
		}
		b.LedgerBalanceMinor += leg.DeltaMinor
		b.AvailableBalanceMinor += leg.DeltaMinor
		txnID := transactionID
		b.LastTransactionID = &txnID
		b.UpdatedAt = time.Now()
	}
	return nil
}

func (f *fakeAccountRepository) AccountNumberExists(_ context.Context, accountNumber string) (bool, error) {
	return f.accountNumbers[accountNumber], nil
}

type fakeLedgerHistoryClient struct {
	entries []StatementEntry
	err     error
}

func (f *fakeLedgerHistoryClient) GetAccountHistory(_ context.Context, _ string, _, _ *time.Time, _ int, _ string) ([]StatementEntry, string, error) {
	if f.err != nil {
		return nil, "", f.err
	}
	return f.entries, "", nil
}
