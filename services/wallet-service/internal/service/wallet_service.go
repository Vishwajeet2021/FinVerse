// Package service implements Wallet Service's business/use-case logic. It
// depends only on domain types and the interfaces in interfaces.go -- never
// on GORM, Gin, or the generated gRPC client directly -- per
// docs/12-coding-standards.md §1.1.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/finverse/wallet-service/internal/domain"
	"github.com/google/uuid"
)

type WalletService struct {
	repo     WalletRepository
	ledger   LedgerClient
	accounts AccountDebitChecker
}

func NewWalletService(repo WalletRepository, ledger LedgerClient, accounts AccountDebitChecker) *WalletService {
	return &WalletService{repo: repo, ledger: ledger, accounts: accounts}
}

// ProvisionWallet implements FR-WAL-1: exactly one wallet per customer. The
// UNIQUE constraint on wallets.customer_id (db/migrations/0001_init.up.sql)
// is the actual non-bypassable guarantee against a concurrent
// double-provision race; this method's own existence check is a fast,
// friendlier pre-check, not the safety net.
//
// A WalletProvisioned event is published even though FR-WAL-1 doesn't
// strictly require one -- it's cheap, and Audit/Notification get a complete
// picture of the wallet lifecycle for free (docs/06-domain-driven-design.md §5).
func (s *WalletService) ProvisionWallet(ctx context.Context, customerID, fundingAccountID string) (*domain.Wallet, error) {
	if existing, err := s.repo.GetByCustomerID(ctx, customerID); err == nil && existing != nil {
		return nil, domain.ErrWalletAlreadyExists
	} else if err != nil && err != domain.ErrWalletNotFound {
		return nil, fmt.Errorf("checking for existing wallet: %w", err)
	}

	wallet := &domain.Wallet{
		ID:               uuid.NewString(),
		CustomerID:       customerID,
		FundingAccountID: fundingAccountID,
		Status:           domain.WalletStatusActive,
		CreatedAt:        time.Now(),
	}
	payload := walletProvisionedPayload{WalletID: wallet.ID, CustomerID: customerID, FundingAccountID: fundingAccountID}

	if err := s.repo.CreateWalletWithProjection(ctx, wallet, EventWalletProvisioned, payload); err != nil {
		return nil, fmt.Errorf("provisioning wallet: %w", err)
	}
	return wallet, nil
}

func (s *WalletService) GetByCustomerID(ctx context.Context, customerID string) (*domain.Wallet, error) {
	return s.repo.GetByCustomerID(ctx, customerID)
}

// GetBalance implements the read side of FR-WAL-2/3 -- reads
// balance_projection directly, never computed live from the ledger
// (docs/05-system-architecture.md §5).
func (s *WalletService) GetBalance(ctx context.Context, walletID string) (*domain.BalanceProjection, error) {
	return s.repo.GetBalance(ctx, walletID)
}

// TopUp implements FR-WAL-2: DEBIT the funding account, CREDIT the wallet,
// posted synchronously via Transaction Service's PostTransaction
// (docs/05-system-architecture.md §4: Wallet<->Transaction is a designated
// sync pair). The balance_projection update itself is NOT applied inline
// here -- it happens when this service's own TransactionPosted consumer
// later processes the resulting event, so there is exactly one code path
// responsible for every projection update, never two racing ones.
func (s *WalletService) TopUp(ctx context.Context, walletID string, amountMinor int64, idempotencyKey string) error {
	if amountMinor <= 0 || amountMinor > domain.MaxAmountMinor {
		return domain.ErrInvalidAmount
	}

	wallet, err := s.repo.GetByID(ctx, walletID)
	if err != nil {
		return err
	}
	if !wallet.IsActive() {
		return domain.ErrWalletClosed
	}
	// The funding account is debited, so it must be allowed to be (FR-ACC-6). Domain errors are
	// returned as they are; an unknown state refuses the top-up instead of assuming it is fine.
	if err := s.accounts.CheckCanDebit(ctx, wallet.FundingAccountID); err != nil {
		if errors.Is(err, domain.ErrFundingAccountFrozen) || errors.Is(err, domain.ErrFundingAccountClosed) {
			return err
		}
		return fmt.Errorf("checking the funding account: %w", err)
	}

	legs := []LedgerLeg{
		{ParticipantID: wallet.FundingAccountID, ParticipantType: ParticipantAccount, AmountMinor: amountMinor, Direction: DirectionDebit, Currency: currencyINR},
		{ParticipantID: wallet.ID, ParticipantType: ParticipantWallet, AmountMinor: amountMinor, Direction: DirectionCredit, Currency: currencyINR},
	}
	transactionID, err := s.ledger.PostTransaction(ctx, idempotencyKey, legs)
	if err != nil {
		return fmt.Errorf("posting top-up to ledger: %w", err)
	}

	payload := walletToppedUpPayload{
		WalletID: wallet.ID, FundingAccountID: wallet.FundingAccountID,
		AmountMinor: amountMinor, Currency: currencyINR, TransactionID: transactionID,
	}
	if err := s.repo.InsertOutboxEvent(ctx, EventWalletToppedUp, payload); err != nil {
		return fmt.Errorf("queuing WalletToppedUp event: %w", err)
	}
	return nil
}

// Transfer implements FR-WAL-3/FR-WAL-4. The insufficient-balance check
// reads this service's OWN balance_projection as a fast pre-check -- it is
// inherently best-effort under concurrency (two simultaneous transfers from
// the same wallet could both pass the check before either's resulting
// TransactionPosted event is consumed and applied) -- a real cross-request
// concurrent-safety guarantee would need either a DB-level balance floor
// enforced by Transaction Service itself (which is deliberately a pure
// ledger with no per-participant limit concept,
// docs/07-microservice-specifications.md §6) or a distributed lock; both are
// documented Phase 3+ hardening items, not built here.
func (s *WalletService) Transfer(ctx context.Context, fromWalletID, toWalletID string, amountMinor int64, idempotencyKey string) error {
	if amountMinor <= 0 || amountMinor > domain.MaxAmountMinor {
		return domain.ErrInvalidAmount
	}
	if fromWalletID == toWalletID {
		return domain.ErrSameWallet
	}

	fromWallet, err := s.repo.GetByID(ctx, fromWalletID)
	if err != nil {
		return err
	}
	if !fromWallet.IsActive() {
		return domain.ErrWalletClosed
	}
	if _, err := s.repo.GetByID(ctx, toWalletID); err != nil {
		return err
	}

	balance, err := s.repo.GetBalance(ctx, fromWalletID)
	if err != nil {
		return fmt.Errorf("checking balance: %w", err)
	}
	if !balance.HasSufficientBalance(amountMinor) {
		return domain.ErrInsufficientBalance
	}

	legs := []LedgerLeg{
		{ParticipantID: fromWalletID, ParticipantType: ParticipantWallet, AmountMinor: amountMinor, Direction: DirectionDebit, Currency: currencyINR},
		{ParticipantID: toWalletID, ParticipantType: ParticipantWallet, AmountMinor: amountMinor, Direction: DirectionCredit, Currency: currencyINR},
	}
	transactionID, err := s.ledger.PostTransaction(ctx, idempotencyKey, legs)
	if err != nil {
		return fmt.Errorf("posting transfer to ledger: %w", err)
	}

	payload := walletTransferredPayload{
		FromWalletID: fromWalletID, ToWalletID: toWalletID,
		AmountMinor: amountMinor, Currency: currencyINR, TransactionID: transactionID,
	}
	if err := s.repo.InsertOutboxEvent(ctx, EventWalletTransferred, payload); err != nil {
		return fmt.Errorf("queuing WalletTransferred event: %w", err)
	}
	return nil
}

// ConsumeTransactionPosted implements the idempotent balance-projection
// update at the heart of this service's correctness
// (docs/05-system-architecture.md §5, §7). A single event can carry more
// than one leg this service owns (both sides of a wallet-to-wallet
// transfer), so every matching delta is applied together, gated by one
// dedupe check for the whole event.
func (s *WalletService) ConsumeTransactionPosted(ctx context.Context, eventID string, payload TransactionPostedPayload) error {
	deltas := toWalletDeltas(payload.Legs)
	if len(deltas) == 0 {
		return nil // no WALLET-type legs in this event -- nothing for this service to do
	}
	applied, err := s.repo.ApplyTransactionPostedIdempotent(ctx, eventID, payload.TransactionID, deltas)
	if err != nil {
		return fmt.Errorf("applying TransactionPosted event %s: %w", eventID, err)
	}
	_ = applied // false just means "already processed" -- not an error, still ack the message
	return nil
}

func toWalletDeltas(legs []LegPayload) []WalletDelta {
	deltas := make([]WalletDelta, 0, len(legs))
	for _, l := range legs {
		if l.ParticipantType != ParticipantWallet {
			continue
		}
		delta := l.AmountMinor
		if l.Direction == DirectionDebit {
			delta = -delta
		}
		deltas = append(deltas, WalletDelta{WalletID: l.ParticipantID, DeltaMinor: delta})
	}
	return deltas
}
