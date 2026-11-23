package service

import (
	"context"
	"time"

	"github.com/finverse/payment-service/internal/domain"
)

// --- async-rail methods on the in-memory payment repository -------------

var claimed = map[*fakePaymentRepository]map[string]time.Time{}

func (f *fakePaymentRepository) leases() map[string]time.Time {
	if claimed[f] == nil {
		claimed[f] = map[string]time.Time{}
	}
	return claimed[f]
}

func (f *fakePaymentRepository) MarkProcessing(_ context.Context, paymentID string, settleAt time.Time) error {
	p, ok := f.byID[paymentID]
	if !ok {
		return domain.ErrPaymentNotFound
	}
	p.Status = domain.StatusProcessing
	p.SettleAt = &settleAt
	return nil
}

func (f *fakePaymentRepository) ClaimDueForSettlement(_ context.Context, now time.Time, lease time.Duration, limit int) ([]*domain.Payment, error) {
	var out []*domain.Payment
	for _, p := range f.byID {
		if len(out) >= limit {
			break
		}
		if p.Status != domain.StatusProcessing || p.SettleAt == nil || p.SettleAt.After(now) {
			continue
		}
		if until, ok := f.leases()[p.ID]; ok && until.After(now) {
			continue
		}
		f.leases()[p.ID] = now.Add(lease)
		p.SettleAttempts++
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}

func (f *fakePaymentRepository) ReleaseForRetry(_ context.Context, paymentID string, retryAt time.Time) error {
	f.leases()[paymentID] = retryAt
	return nil
}

// --- schedule repository --------------------------------------------------

type fakeScheduleRepo struct {
	scheduled map[string]*domain.ScheduledPayment
	standing  map[string]*domain.StandingInstruction
	leases    map[string]time.Time
}

func newFakeScheduleRepo() *fakeScheduleRepo {
	return &fakeScheduleRepo{scheduled: map[string]*domain.ScheduledPayment{}, standing: map[string]*domain.StandingInstruction{}, leases: map[string]time.Time{}}
}

func (f *fakeScheduleRepo) CreateScheduled(_ context.Context, sp *domain.ScheduledPayment) error {
	cp := *sp
	f.scheduled[sp.ID] = &cp
	return nil
}
func (f *fakeScheduleRepo) ListScheduled(_ context.Context, by string, _ int) ([]domain.ScheduledPayment, error) {
	var out []domain.ScheduledPayment
	for _, s := range f.scheduled {
		if s.CreatedBy == by {
			out = append(out, *s)
		}
	}
	return out, nil
}
func (f *fakeScheduleRepo) CancelScheduled(_ context.Context, by, id string) error {
	s, ok := f.scheduled[id]
	if !ok || s.CreatedBy != by {
		return domain.ErrScheduleNotFound
	}
	if s.Status != "PENDING" {
		return domain.ErrScheduleNotPending
	}
	s.Status = "CANCELLED"
	return nil
}
func (f *fakeScheduleRepo) ClaimDueScheduled(_ context.Context, now time.Time, lease time.Duration, limit int) ([]domain.ScheduledPayment, error) {
	var out []domain.ScheduledPayment
	for _, s := range f.scheduled {
		if s.Status != "PENDING" || s.RunAt.After(now) || len(out) >= limit {
			continue
		}
		if until, ok := f.leases[s.ID]; ok && until.After(now) {
			continue
		}
		f.leases[s.ID] = now.Add(lease)
		out = append(out, *s)
	}
	return out, nil
}
func (f *fakeScheduleRepo) CompleteScheduled(_ context.Context, id, paymentID string) error {
	s := f.scheduled[id]
	s.Status, s.PaymentID = "EXECUTED", &paymentID
	return nil
}
func (f *fakeScheduleRepo) ReleaseScheduled(_ context.Context, id string, retryAt time.Time) error {
	f.leases[id] = retryAt
	return nil
}

func (f *fakeScheduleRepo) CreateStanding(_ context.Context, si *domain.StandingInstruction) error {
	cp := *si
	f.standing[si.ID] = &cp
	return nil
}
func (f *fakeScheduleRepo) ListStanding(_ context.Context, by string, _ int) ([]domain.StandingInstruction, error) {
	var out []domain.StandingInstruction
	for _, s := range f.standing {
		if s.CreatedBy == by {
			out = append(out, *s)
		}
	}
	return out, nil
}
func (f *fakeScheduleRepo) GetStanding(_ context.Context, by, id string) (*domain.StandingInstruction, error) {
	s, ok := f.standing[id]
	if !ok || s.CreatedBy != by {
		return nil, domain.ErrScheduleNotFound
	}
	cp := *s
	return &cp, nil
}
func (f *fakeScheduleRepo) SetStandingStatus(_ context.Context, by, id, status string, next *time.Time) error {
	s, ok := f.standing[id]
	if !ok || s.CreatedBy != by {
		return domain.ErrScheduleNotFound
	}
	if s.Status == "CANCELLED" {
		return domain.ErrScheduleNotPending
	}
	s.Status = status
	if next != nil {
		s.NextRunAt = *next
	}
	return nil
}
func (f *fakeScheduleRepo) ClaimDueStanding(_ context.Context, now time.Time, lease time.Duration, limit int) ([]domain.StandingInstruction, error) {
	var out []domain.StandingInstruction
	for _, s := range f.standing {
		if s.Status != "ACTIVE" || s.NextRunAt.After(now) || len(out) >= limit {
			continue
		}
		if until, ok := f.leases[s.ID]; ok && until.After(now) {
			continue
		}
		f.leases[s.ID] = now.Add(lease)
		out = append(out, *s)
	}
	return out, nil
}
func (f *fakeScheduleRepo) AdvanceStanding(_ context.Context, id string, next time.Time, lastPaymentID string) error {
	s := f.standing[id]
	s.NextRunAt, s.LastPaymentID = next, &lastPaymentID
	delete(f.leases, id)
	return nil
}
func (f *fakeScheduleRepo) ReleaseStanding(_ context.Context, id string, retryAt time.Time) error {
	f.leases[id] = retryAt
	return nil
}
