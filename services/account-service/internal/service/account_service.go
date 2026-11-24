package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/finverse/account-service/internal/domain"
	"github.com/google/uuid"
)

// maxStatementPages bounds GetStatement's cursor-following loop against
// Transaction Service. FR-ACC-7 asks for "an arbitrary date range," but this
// service has no independent copy of ledger history to page through on its
// own (docs/07-microservice-specifications.md §4) — a genuinely huge range
// belongs to Reporting Service's async generation path (§15), not a
// synchronous REST call, so this is a deliberate, documented cap rather than
// unbounded fan-out against Transaction Service.
const maxStatementPages = 5

const statementPageSize = 200

type AccountService struct {
	repo   AccountRepository
	ledger LedgerHistoryClient
}

func NewAccountService(repo AccountRepository, ledger LedgerHistoryClient) *AccountService {
	return &AccountService{repo: repo, ledger: ledger}
}

// OpenAccount implements FR-ACC-1: creates the account, its OWNER holder
// row, and a zero-balance projection row atomically, publishing
// AccountOpened via the outbox (docs/05-system-architecture.md §7).
//
// FR-ACC-1 also requires the caller be Tier 1+; that check is deliberately
// not here — it's enforced in internal/transport/http using the kyc_tier
// claim already embedded in the caller's JWT (docs/10-authentication-authorization.md §2.1),
// so this service method stays a pure "given a valid customer and product
// type, open the account" operation and doesn't need to know about claims.
func (s *AccountService) OpenAccount(ctx context.Context, customerID, productType string) (*domain.Account, error) {
	pt := domain.ProductType(strings.ToUpper(strings.TrimSpace(productType)))
	if !pt.Valid() {
		return nil, domain.ErrInvalidProductType
	}

	var lastErr error
	for attempt := 0; attempt < maxAccountNumberAttempts; attempt++ {
		accountNumber, err := generateAccountNumber()
		if err != nil {
			return nil, fmt.Errorf("generating account number: %w", err)
		}

		account := &domain.Account{
			ID:                  uuid.NewString(),
			CustomerID:          customerID,
			AccountNumber:       accountNumber,
			ProductType:         pt,
			Status:              domain.StatusActive,
			OverdraftLimitMinor: 0,
			CreatedAt:           time.Now(),
		}
		holder := &domain.AccountHolder{
			AccountID:  account.ID,
			CustomerID: customerID,
			Role:       domain.RoleOwner,
		}
		payload := accountOpenedPayload{
			AccountID:     account.ID,
			CustomerID:    customerID,
			AccountNumber: accountNumber,
			ProductType:   string(pt),
		}

		err = s.repo.OpenAccountWithOutbox(ctx, account, holder, EventAccountOpened, payload)
		if err == nil {
			return account, nil
		}
		if !errors.Is(err, domain.ErrAccountNumberCollision) {
			return nil, fmt.Errorf("opening account: %w", err)
		}
		lastErr = err // collided — loop and try a freshly generated number
	}
	return nil, fmt.Errorf("opening account: exhausted %d account number generation attempts: %w", maxAccountNumberAttempts, lastErr)
}

// GetAccount implements the read side of FR-ACC-1/FR-ACC-5's status
// visibility. Ownership is checked by the caller (internal/transport/http),
// via IsAccountHolder, per docs/10-authentication-authorization.md §3.3.
func (s *AccountService) GetAccount(ctx context.Context, accountID string) (*domain.Account, error) {
	return s.repo.GetByID(ctx, accountID)
}

// ResolveAccountNumber implements the internal account-number -> account-ID
// lookup that Payment Service calls before posting a transfer — a payer
// knows the recipient's public account number, never the internal UUID
// (docs/07-microservice-specifications.md §7).
func (s *AccountService) ResolveAccountNumber(ctx context.Context, accountNumber string) (*domain.Account, error) {
	return s.repo.GetByAccountNumber(ctx, accountNumber)
}

// ListMyAccounts returns every account the given customer holds. Unlike
// GetAccount, there's no separate ownership check to perform here —
// ListByCustomerID is scoped to the caller's own customerID by construction.
func (s *AccountService) ListMyAccounts(ctx context.Context, customerID string) ([]domain.Account, error) {
	return s.repo.ListByCustomerID(ctx, customerID)
}

// IsAccountHolder implements the ownership check docs/10-authentication-authorization.md §3.3
// requires every handler perform before returning or mutating a specific
// resource — "a scope check alone is not enough."
func (s *AccountService) IsAccountHolder(ctx context.Context, accountID, customerID string) (bool, error) {
	_, err := s.repo.GetHolder(ctx, accountID, customerID)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, domain.ErrNotAccountHolder) {
		return false, nil
	}
	return false, err
}

// GetBalance implements FR-ACC-3: reads the balance_projection row directly
// — never computed live from the ledger, per docs/05-system-architecture.md §5.
func (s *AccountService) GetBalance(ctx context.Context, accountID string) (*domain.BalanceProjection, error) {
	return s.repo.GetBalance(ctx, accountID)
}

// Freeze implements FR-ACC-5/FR-ACC-6: flips status to FROZEN and publishes
// AccountFrozen with the reason so Notification/Audit can record it
// (docs/06-domain-driven-design.md §5) — the accounts table itself carries
// no freeze-reason column (docs/08-database-design.md §5 defines none), so
// the event is the durable record of "why."
//
// Freeze is the customer path; Admin Service uses FreezeAs with ActorAdmin
// through the internal route (docs/23 D-A4).
func (s *AccountService) Freeze(ctx context.Context, accountID, reason string) error {
	return s.FreezeAs(ctx, accountID, reason, domain.ActorCustomer)
}

// FreezeAs freezes on behalf of actor. The actor is recorded on the account so a
// customer cannot later lift an administrator's hold (docs/23 D-A4).
func (s *AccountService) FreezeAs(ctx context.Context, accountID, reason string, actor domain.Actor) error {
	account, err := s.repo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if account.Status == domain.StatusClosed {
		return domain.ErrAccountClosed
	}
	if account.Status == domain.StatusFrozen {
		return domain.ErrAccountAlreadyFrozen
	}

	payload := accountFrozenPayload{AccountID: accountID, Reason: reason, Actor: string(actor)}
	if err := s.repo.UpdateStatusWithOutbox(ctx, accountID, domain.StatusFrozen, actor, EventAccountFrozen, payload); err != nil {
		return fmt.Errorf("freezing account: %w", err)
	}
	return nil
}

// Unfreeze implements FR-ACC-5's reverse transition on behalf of the customer.
func (s *AccountService) Unfreeze(ctx context.Context, accountID string) error {
	return s.UnfreezeAs(ctx, accountID, domain.ActorCustomer)
}

// UnfreezeAs lifts a freeze. A customer may not lift one an administrator placed.
func (s *AccountService) UnfreezeAs(ctx context.Context, accountID string, actor domain.Actor) error {
	account, err := s.repo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if account.Status != domain.StatusFrozen {
		return domain.ErrAccountNotFrozen
	}
	if account.FrozenBy == domain.ActorAdmin && actor != domain.ActorAdmin {
		return domain.ErrFrozenByAdmin
	}

	payload := accountUnfrozenPayload{AccountID: accountID, Actor: string(actor)}
	if err := s.repo.UpdateStatusWithOutbox(ctx, accountID, domain.StatusActive, "", EventAccountUnfrozen, payload); err != nil {
		return fmt.Errorf("unfreezing account: %w", err)
	}
	return nil
}

// CloseAccount implements FR-ACC-5's terminal transition: ACTIVE or FROZEN
// -> CLOSED. Closing is permanent -- there's no Reopen, unlike
// Freeze/Unfreeze -- so it's gated on a zero ledger balance
// (domain.ErrAccountHasBalance) the same way a real bank refuses to close an
// account out from under its own money. The caller (or the customer,
// through a real transfer) must empty the account first; Account Service
// has no way to move funds itself (docs/07-microservice-specifications.md §4
// -- it never posts to the ledger).
func (s *AccountService) CloseAccount(ctx context.Context, accountID string) error {
	account, err := s.repo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if account.Status == domain.StatusClosed {
		return domain.ErrAccountAlreadyClosed
	}

	balance, err := s.repo.GetBalance(ctx, accountID)
	if err != nil {
		return fmt.Errorf("checking balance before close: %w", err)
	}
	if balance.LedgerBalanceMinor != 0 {
		return domain.ErrAccountHasBalance
	}

	payload := accountClosedPayload{AccountID: accountID, Actor: "customer"}
	if err := s.repo.UpdateStatusWithOutbox(ctx, accountID, domain.StatusClosed, "", EventAccountClosed, payload); err != nil {
		return fmt.Errorf("closing account: %w", err)
	}
	return nil
}

// GetStatement implements FR-ACC-7: statements are sourced from Transaction
// Service's ledger history, not from anything stored locally — Account
// Service only keeps the current-balance projection
// (docs/05-system-architecture.md §5), never a copy of full history.
func (s *AccountService) GetStatement(ctx context.Context, accountID string, from, to time.Time) ([]StatementEntry, error) {
	if _, err := s.repo.GetByID(ctx, accountID); err != nil {
		return nil, err
	}

	var fromPtr, toPtr *time.Time
	if !from.IsZero() {
		fromPtr = &from
	}
	if !to.IsZero() {
		toPtr = &to
	}

	var all []StatementEntry
	cursor := ""
	for page := 0; page < maxStatementPages; page++ {
		entries, nextCursor, err := s.ledger.GetAccountHistory(ctx, accountID, fromPtr, toPtr, statementPageSize, cursor)
		if err != nil {
			return nil, fmt.Errorf("fetching statement history: %w", err)
		}
		all = append(all, entries...)
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return all, nil
}

// ConsumeTransactionPosted implements the idempotent balance-projection
// update at the heart of this service's correctness
// (docs/05-system-architecture.md §5, §7). eventID is the envelope's
// dedupe key (docs/09-api-standards.md §4.1) — the repository applies it
// and every leg's balance delta in one DB transaction, so a redelivered
// event (Redis Streams consumer groups are at-least-once) is a guaranteed
// no-op on replay, never a double-counted balance.
func (s *AccountService) ConsumeTransactionPosted(ctx context.Context, eventID string, payload TransactionPostedPayload) error {
	legs := toLegUpdates(payload.Legs)
	if len(legs) == 0 {
		return nil // no ACCOUNT-type legs in this event — nothing for this service to do
	}
	if err := s.repo.ApplyTransactionPosted(ctx, eventID, payload.TransactionID, legs); err != nil {
		return fmt.Errorf("applying TransactionPosted event %s: %w", eventID, err)
	}
	return nil
}
