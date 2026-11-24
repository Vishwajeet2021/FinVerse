package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/finverse/payment-service/internal/domain"
	"github.com/google/uuid"
)

var errSchedulesDisabled = errors.New("scheduled payments are not enabled")

const (
	scheduleLease        = 60 * time.Second
	scheduleBatch        = 50
	scheduleRetryBackoff = 30 * time.Second
)

type ScheduleRequest struct {
	CreatedBy       string
	FromAccountID   string
	ToAccountNumber string
	AmountMinor     int64
	Rail            domain.Rail
}

// ScheduleOnce implements FR-PAY-2: a one-time payment at a future time.
// The caller has already verified the user holds FromAccountID.
func (s *PaymentService) ScheduleOnce(ctx context.Context, r ScheduleRequest, runAt time.Time) (*domain.ScheduledPayment, error) {
	if s.schedules == nil {
		return nil, errSchedulesDisabled
	}
	if r.Rail == "" {
		r.Rail = domain.RailInternal
	}
	if err := s.validateSchedule(ctx, r); err != nil {
		return nil, err
	}
	if !runAt.After(s.now()) {
		return nil, domain.ErrScheduleInPast
	}
	sp := &domain.ScheduledPayment{
		ID: uuid.NewString(), CreatedBy: r.CreatedBy, FromAccountID: r.FromAccountID, ToReference: r.ToAccountNumber,
		Rail: r.Rail, AmountMinor: r.AmountMinor, RunAt: runAt.UTC(), Status: "PENDING", CreatedAt: s.now(),
	}
	if err := s.schedules.CreateScheduled(ctx, sp); err != nil {
		return nil, fmt.Errorf("creating scheduled payment: %w", err)
	}
	return sp, nil
}

// CreateStanding implements FR-PAY-3: a recurring payment.
func (s *PaymentService) CreateStanding(ctx context.Context, r ScheduleRequest, freq domain.Frequency, firstRunAt time.Time) (*domain.StandingInstruction, error) {
	if s.schedules == nil {
		return nil, errSchedulesDisabled
	}
	if r.Rail == "" {
		r.Rail = domain.RailInternal
	}
	if !freq.Valid() {
		return nil, domain.ErrInvalidFrequency
	}
	if err := s.validateSchedule(ctx, r); err != nil {
		return nil, err
	}
	if !firstRunAt.After(s.now()) {
		return nil, domain.ErrScheduleInPast
	}
	si := &domain.StandingInstruction{
		ID: uuid.NewString(), CreatedBy: r.CreatedBy, FromAccountID: r.FromAccountID, ToReference: r.ToAccountNumber,
		Rail: r.Rail, AmountMinor: r.AmountMinor, Frequency: freq, NextRunAt: firstRunAt.UTC(),
		AnchorDay: firstRunAt.UTC().Day(), Status: "ACTIVE", CreatedAt: s.now(),
	}
	if err := s.schedules.CreateStanding(ctx, si); err != nil {
		return nil, fmt.Errorf("creating standing instruction: %w", err)
	}
	return si, nil
}

func (s *PaymentService) validateSchedule(ctx context.Context, r ScheduleRequest) error {
	if r.AmountMinor <= 0 || r.AmountMinor > domain.MaxAmountMinor {
		return domain.ErrInvalidAmount
	}
	if err := s.rails.Validate(r.Rail, r.AmountMinor); err != nil {
		return err
	}
	toID, err := s.account.ResolveAccountID(ctx, r.ToAccountNumber) // reject a typo'd recipient now, not months later
	if err != nil {
		return err
	}
	if toID == r.FromAccountID {
		return domain.ErrSameAccount
	}
	return nil
}

func (s *PaymentService) ListScheduled(ctx context.Context, createdBy string) ([]domain.ScheduledPayment, error) {
	if s.schedules == nil {
		return nil, errSchedulesDisabled
	}
	return s.schedules.ListScheduled(ctx, createdBy, 100)
}

func (s *PaymentService) CancelScheduled(ctx context.Context, createdBy, id string) error {
	if s.schedules == nil {
		return errSchedulesDisabled
	}
	return s.schedules.CancelScheduled(ctx, createdBy, id)
}

func (s *PaymentService) ListStanding(ctx context.Context, createdBy string) ([]domain.StandingInstruction, error) {
	if s.schedules == nil {
		return nil, errSchedulesDisabled
	}
	return s.schedules.ListStanding(ctx, createdBy, 100)
}

func (s *PaymentService) PauseStanding(ctx context.Context, createdBy, id string) error {
	if s.schedules == nil {
		return errSchedulesDisabled
	}
	return s.schedules.SetStandingStatus(ctx, createdBy, id, "PAUSED", nil)
}

// ResumeStanding reactivates a paused instruction. Its next run is moved past
// now first, so resuming never fires the runs missed while paused.
func (s *PaymentService) ResumeStanding(ctx context.Context, createdBy, id string) error {
	if s.schedules == nil {
		return errSchedulesDisabled
	}
	si, err := s.schedules.GetStanding(ctx, createdBy, id)
	if err != nil {
		return err
	}
	next := domain.AdvancePast(si.NextRunAt, s.now(), si.Frequency, si.AnchorDay)
	return s.schedules.SetStandingStatus(ctx, createdBy, id, "ACTIVE", &next)
}

func (s *PaymentService) CancelStanding(ctx context.Context, createdBy, id string) error {
	if s.schedules == nil {
		return errSchedulesDisabled
	}
	return s.schedules.SetStandingStatus(ctx, createdBy, id, "CANCELLED", nil)
}

// RunDueSchedules executes every scheduled payment and standing instruction
// whose time has come. Each execution goes through the normal payment path
// (fraud screening, rail limits, ledger) with an idempotency key derived from
// the schedule and run time: if a replica crashes mid-way, the retry returns
// the payment already created instead of paying twice.
func (s *PaymentService) RunDueSchedules(ctx context.Context) (int, error) {
	if s.schedules == nil {
		return 0, nil
	}
	now := s.now()
	executed := 0

	scheduled, err := s.schedules.ClaimDueScheduled(ctx, now, scheduleLease, scheduleBatch)
	if err != nil {
		return 0, fmt.Errorf("claiming due scheduled payments: %w", err)
	}
	for _, sp := range scheduled {
		p, err := s.InitiatePaymentOnRail(ctx, sp.Rail, sp.FromAccountID, sp.ToReference, sp.AmountMinor, "sched-"+sp.ID)
		if p == nil { // infrastructure error before a payment record existed: try again later
			_ = s.schedules.ReleaseScheduled(ctx, sp.ID, now.Add(scheduleRetryBackoff))
			_ = err
			continue
		}
		// A payment that FAILED (fraud denial, ...) is still the outcome of this
		// schedule: it is recorded against it, and the payment shows why.
		if err := s.schedules.CompleteScheduled(ctx, sp.ID, p.ID); err == nil {
			executed++
		}
	}

	standing, err := s.schedules.ClaimDueStanding(ctx, now, scheduleLease, scheduleBatch)
	if err != nil {
		return executed, fmt.Errorf("claiming due standing instructions: %w", err)
	}
	for _, si := range standing {
		key := fmt.Sprintf("si-%s-%d", si.ID, si.NextRunAt.Unix())
		p, _ := s.InitiatePaymentOnRail(ctx, si.Rail, si.FromAccountID, si.ToReference, si.AmountMinor, key)
		if p == nil {
			_ = s.schedules.ReleaseStanding(ctx, si.ID, now.Add(scheduleRetryBackoff))
			continue
		}
		next := domain.AdvancePast(si.NextRunAt, now, si.Frequency, si.AnchorDay)
		if err := s.schedules.AdvanceStanding(ctx, si.ID, next, p.ID); err == nil {
			executed++
		}
	}
	return executed, nil
}
