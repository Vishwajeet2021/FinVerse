package service

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/finverse/payment-service/internal/domain"
	"github.com/google/uuid"
)

// ledgerCallTimeout bounds the synchronous PostTransaction call. FR-PAY /
// NFR-PERF-3 budgets the whole synchronous fund-transfer leg at 500ms; this
// gRPC hop is the dominant cost of that budget, so its own timeout is set
// tighter than the overall budget, not equal to it. Local dev value; a real
// deployment would tune this against measured P99s
// (docs/04-non-functional-requirements.md).
const ledgerCallTimeout = 2 * time.Second

// fraudCallTimeout bounds the synchronous fraud check. NFR-PERF-4 budgets the
// check itself at 80ms P99; this is the ceiling after which the verdict is
// treated as unknown (and the fail-open/closed policy applies).
const fraudCallTimeout = 300 * time.Millisecond

type PaymentService struct {
	repo    PaymentRepository
	ledger  LedgerClient
	account AccountResolver
	fraud   FraudChecker
	// failOpen decides what an unknown fraud verdict (Fraud Service down or
	// slow) means: false (default) rejects the payment - safe for money, but
	// a Fraud outage then blocks payments; true lets it through unchecked.
	failOpen bool

	rails     RailConfig
	schedules ScheduleRepository
	rng       func() float64 // simulated rail rejection; overridable in tests
	now       func() time.Time
}

func NewPaymentService(repo PaymentRepository, ledger LedgerClient, account AccountResolver, fraud FraudChecker, failOpen bool) *PaymentService {
	return &PaymentService{repo: repo, ledger: ledger, account: account, fraud: fraud, failOpen: failOpen,
		rails: DefaultRailConfig(), rng: rand.Float64, now: time.Now}
}

// WithRails sets the rail simulation parameters.
func (s *PaymentService) WithRails(c RailConfig) *PaymentService { s.rails = c; return s }

// WithSchedules enables scheduled payments and standing instructions.
func (s *PaymentService) WithSchedules(r ScheduleRepository) *PaymentService {
	s.schedules = r
	return s
}

// InitiatePayment implements FR-PAY-1 (INTERNAL rail only, per
// docs/19-implementation-roadmap.md Phase 2) and the fund-transfer sequence
// in docs/05-system-architecture.md §6.1, minus the Fraud hop -- Fraud
// Service is Phase 3 and does not exist yet; this is a deliberate,
// documented gap, not a silent omission. OTP step-up (FR-PAY-4) is likewise
// deferred -- Auth Service's OTP endpoints aren't built yet either.
//
// Ownership of fromAccountID is NOT verified here (this service has no way
// to check Account Service's account_holders table, and Account Service
// isn't guaranteed to exist in every environment this runs in) -- it trusts
// the caller-supplied ID, which is safe today only because nothing calls
// this except the frontend acting on the authenticated user's own account.
// A real hardening pass adds a call to Account Service to verify holder
// ownership before initiating; tracked as a Phase 3 item.
func (s *PaymentService) InitiatePayment(ctx context.Context, fromAccountID, toAccountNumber string, amountMinor int64, idempotencyKey string) (*domain.Payment, error) {
	return s.InitiatePaymentOnRail(ctx, domain.RailInternal, fromAccountID, toAccountNumber, amountMinor, idempotencyKey)
}

// InitiatePaymentOnRail is InitiatePayment for any rail. INTERNAL posts to the
// ledger inside the call. The simulated rails (UPI/IMPS/NEFT/RTGS) pass the
// same validation and fraud screening, then stop at PROCESSING; the settlement
// worker (SettleDue) posts the ledger entry when the rail "clears" the payment.
func (s *PaymentService) InitiatePaymentOnRail(ctx context.Context, rail domain.Rail, fromAccountID, toAccountNumber string, amountMinor int64, idempotencyKey string) (*domain.Payment, error) {
	if existing, err := s.repo.GetByIdempotencyKey(ctx, fromAccountID, idempotencyKey); err == nil && existing != nil {
		return existing, nil // FR-TXN-3-style semantics: duplicate request returns the original result
	} else if err != nil && err != domain.ErrPaymentNotFound {
		return nil, fmt.Errorf("checking idempotency key: %w", err)
	}

	if amountMinor <= 0 || amountMinor > domain.MaxAmountMinor {
		return nil, domain.ErrInvalidAmount
	}
	if err := s.rails.Validate(rail, amountMinor); err != nil {
		return nil, err
	}

	// A payer only ever knows the recipient's public account number, never
	// Account Service's internal account ID -- resolve it before creating
	// any payment record at all, the same way the amount/same-account
	// checks above reject a fundamentally invalid request before it leaves
	// any trace. See internal/client/account and
	// services/account-service/internal/transport/http/handlers.go's
	// ResolveAccountNumber for why this lookup exists.
	toAccountID, err := s.account.ResolveAccountID(ctx, toAccountNumber)
	if err != nil {
		if err == domain.ErrRecipientAccountNotFound {
			return nil, err
		}
		return nil, fmt.Errorf("resolving recipient account number: %w", err)
	}
	if fromAccountID == toAccountID {
		return nil, domain.ErrSameAccount
	}

	payment := &domain.Payment{
		ID:             uuid.NewString(),
		IdempotencyKey: idempotencyKey,
		FromAccountID:  fromAccountID,
		ToReference:    toAccountNumber,
		Rail:           rail,
		AmountMinor:    amountMinor,
		Status:         domain.StatusInitiated,
		CreatedAt:      time.Now(),
	}
	// PROCESSING is not materialized as its own DB write: this entire flow
	// is one synchronous request/response cycle, so there's no observer
	// between INITIATED and the terminal state that would ever see it --
	// recording it would be an extra round trip with no one to read it.
	if err := s.repo.Create(ctx, payment); err != nil {
		return nil, fmt.Errorf("creating payment: %w", err)
	}
	if err := s.repo.InsertOutboxEvent(ctx, EventPaymentInitiated, paymentInitiatedPayload{
		PaymentID: payment.ID, FromAccountID: fromAccountID, ToReference: toAccountNumber,
		AmountMinor: amountMinor, Rail: string(rail),
	}); err != nil {
		return nil, fmt.Errorf("queuing PaymentInitiated event: %w", err)
	}

	// Synchronous fraud check (docs/05-system-architecture.md 6.1): after the
	// payment is recorded (so a denial is auditable) and before any money moves.
	fraudCtx, cancelFraud := context.WithTimeout(ctx, fraudCallTimeout)
	verdict, fraudErr := s.fraud.CheckTransaction(fraudCtx, payment.ID, fromAccountID, toAccountNumber, amountMinor, string(rail), idempotencyKey)
	cancelFraud()
	switch {
	case fraudErr != nil && !s.failOpen:
		return s.failPayment(ctx, payment, "FRAUD_CHECK_UNAVAILABLE: risk check could not be completed, please try again", fmt.Errorf("fraud check: %w", fraudErr))
	case fraudErr == nil && verdict.Decision == FraudDeny:
		return s.failPayment(ctx, payment, "FRAUD_DENIED: "+verdict.Reason, nil)
	}

	if IsAsync(rail) {
		settleAt := s.rails.SettleAt(rail, s.now())
		if err := s.repo.MarkProcessing(ctx, payment.ID, settleAt); err != nil {
			return nil, fmt.Errorf("marking payment processing: %w", err)
		}
		payment.Status = domain.StatusProcessing
		payment.SettleAt = &settleAt
		return payment, nil
	}

	legs := []LedgerLeg{
		{ParticipantID: fromAccountID, ParticipantType: ParticipantAccount, AmountMinor: amountMinor, Direction: DirectionDebit, Currency: currencyINR},
		{ParticipantID: toAccountID, ParticipantType: ParticipantAccount, AmountMinor: amountMinor, Direction: DirectionCredit, Currency: currencyINR},
	}

	ledgerCtx, cancel := context.WithTimeout(ctx, ledgerCallTimeout)
	transactionID, err := s.ledger.PostTransaction(ledgerCtx, idempotencyKey, legs)
	cancel()

	if err != nil {
		reason := err.Error()
		if markErr := s.repo.MarkFailedWithOutbox(ctx, payment.ID, reason, EventPaymentFailed, paymentFailedPayload{PaymentID: payment.ID, Reason: reason}); markErr != nil {
			return nil, fmt.Errorf("posting to ledger failed (%w) and marking payment failed also failed: %w", err, markErr)
		}
		payment.Status = domain.StatusFailed
		payment.FailureReason = &reason
		return payment, fmt.Errorf("posting payment to ledger: %w", err)
	}

	if err := s.repo.MarkCompletedWithOutbox(ctx, payment.ID, transactionID, EventPaymentCompleted, paymentCompletedPayload{PaymentID: payment.ID, TransactionID: transactionID}); err != nil {
		return nil, fmt.Errorf("marking payment completed: %w", err)
	}
	payment.Status = domain.StatusCompleted
	payment.TransactionID = &transactionID
	return payment, nil
}

// failPayment records a terminal FAILED state plus the PaymentFailed event and
// returns the payment (the HTTP layer reports the outcome via its status, like
// any other failed payment).
func (s *PaymentService) failPayment(ctx context.Context, payment *domain.Payment, reason string, cause error) (*domain.Payment, error) {
	if markErr := s.repo.MarkFailedWithOutbox(ctx, payment.ID, reason, EventPaymentFailed, paymentFailedPayload{PaymentID: payment.ID, Reason: reason}); markErr != nil {
		return nil, fmt.Errorf("marking payment failed: %w", markErr)
	}
	payment.Status = domain.StatusFailed
	payment.FailureReason = &reason
	return payment, cause
}

// GetPayment implements FR-PAY-6's status polling.
func (s *PaymentService) GetPayment(ctx context.Context, paymentID string) (*domain.Payment, error) {
	return s.repo.GetByID(ctx, paymentID)
}
