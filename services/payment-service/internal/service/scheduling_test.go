package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/finverse/payment-service/internal/domain"
)

func newScheduleService(t *testing.T) (*PaymentService, *fakePaymentRepository, *fakeScheduleRepo, *fakeLedgerClient, *fakeFraudChecker, *clock) {
	t.Helper()
	svc, repo, ledger, fraud, c := newRailService(t)
	sched := newFakeScheduleRepo()
	svc.WithSchedules(sched)
	return svc, repo, sched, ledger, fraud, c
}

var req = ScheduleRequest{CreatedBy: "user-1", FromAccountID: "acct-1", ToAccountNumber: "acct-2", AmountMinor: 5_000}

func TestScheduleOnceValidation(t *testing.T) {
	svc, _, _, _, _, c := newScheduleService(t)
	ctx := context.Background()
	if _, err := svc.ScheduleOnce(ctx, req, c.t.Add(-time.Minute)); !errors.Is(err, domain.ErrScheduleInPast) {
		t.Errorf("past: %v", err)
	}
	bad := req
	bad.AmountMinor = 0
	if _, err := svc.ScheduleOnce(ctx, bad, c.t.Add(time.Hour)); !errors.Is(err, domain.ErrInvalidAmount) {
		t.Errorf("amount: %v", err)
	}
	bad = req
	bad.Rail, bad.AmountMinor = domain.RailRTGS, 100
	if _, err := svc.ScheduleOnce(ctx, bad, c.t.Add(time.Hour)); !errors.Is(err, domain.ErrRailLimit) {
		t.Errorf("rail limit must be rejected at scheduling time, not months later: %v", err)
	}
	bad = req
	bad.ToAccountNumber = "acct-1"
	if _, err := svc.ScheduleOnce(ctx, bad, c.t.Add(time.Hour)); !errors.Is(err, domain.ErrSameAccount) {
		t.Errorf("same account: %v", err)
	}
}

func TestScheduledPaymentRunsOnceAtItsTime(t *testing.T) {
	svc, repo, sched, ledger, _, c := newScheduleService(t)
	ctx := context.Background()
	sp, err := svc.ScheduleOnce(ctx, req, c.t.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := svc.RunDueSchedules(ctx); n != 0 || len(ledger.calls) != 0 {
		t.Fatal("ran before its time")
	}

	c.t = c.t.Add(time.Hour + time.Second)
	if n, err := svc.RunDueSchedules(ctx); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if len(ledger.calls) != 1 || ledger.calls[0].IdempotencyKey != "sched-"+sp.ID {
		t.Fatalf("ledger calls %+v", ledger.calls)
	}
	got := sched.scheduled[sp.ID]
	if got.Status != "EXECUTED" || got.PaymentID == nil || repo.byID[*got.PaymentID].Status != domain.StatusCompleted {
		t.Fatalf("schedule %+v", got)
	}
	c.t = c.t.Add(time.Hour)
	_, _ = svc.RunDueSchedules(ctx)
	if len(ledger.calls) != 1 {
		t.Errorf("a one-time payment ran %d times", len(ledger.calls))
	}
}

// The crash-safety property: if the process dies after creating the payment but
// before marking the schedule EXECUTED, the retry must return the SAME payment.
func TestScheduledPaymentExecutionIsIdempotentAcrossACrash(t *testing.T) {
	svc, repo, sched, ledger, _, c := newScheduleService(t)
	ctx := context.Background()
	sp, _ := svc.ScheduleOnce(ctx, req, c.t.Add(time.Hour))
	c.t = c.t.Add(2 * time.Hour)

	first, _ := svc.InitiatePaymentOnRail(ctx, sp.Rail, sp.FromAccountID, sp.ToReference, sp.AmountMinor, "sched-"+sp.ID)
	// ...crash here: schedule still PENDING with an expired lease...
	if sched.scheduled[sp.ID].Status != "PENDING" {
		t.Fatal("setup")
	}
	_, _ = svc.RunDueSchedules(ctx)

	if len(ledger.calls) != 1 {
		t.Errorf("paid %d times after a crash-retry", len(ledger.calls))
	}
	if len(repo.byID) != 1 || *sched.scheduled[sp.ID].PaymentID != first.ID {
		t.Errorf("retry created a different payment")
	}
}

func TestScheduledPaymentThatFailsFraudStillRecordsTheFailedPayment(t *testing.T) {
	svc, repo, sched, ledger, fraud, c := newScheduleService(t)
	ctx := context.Background()
	sp, _ := svc.ScheduleOnce(ctx, req, c.t.Add(time.Hour))
	fraud.verdict = FraudVerdict{Decision: FraudDeny, Reason: "limit"}
	c.t = c.t.Add(2 * time.Hour)
	_, _ = svc.RunDueSchedules(ctx)

	got := sched.scheduled[sp.ID]
	if got.PaymentID == nil || repo.byID[*got.PaymentID].Status != domain.StatusFailed || len(ledger.calls) != 0 {
		t.Fatalf("schedule %+v", got)
	}
}

func TestCancelledScheduleNeverRuns(t *testing.T) {
	svc, _, _, ledger, _, c := newScheduleService(t)
	ctx := context.Background()
	sp, _ := svc.ScheduleOnce(ctx, req, c.t.Add(time.Hour))
	if err := svc.CancelScheduled(ctx, "someone-else", sp.ID); !errors.Is(err, domain.ErrScheduleNotFound) {
		t.Errorf("another user cancelled it: %v", err)
	}
	if err := svc.CancelScheduled(ctx, "user-1", sp.ID); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(2 * time.Hour)
	_, _ = svc.RunDueSchedules(ctx)
	if len(ledger.calls) != 0 {
		t.Error("cancelled payment ran")
	}
}

func TestStandingInstructionRunsEachPeriodWithDistinctKeys(t *testing.T) {
	svc, _, sched, ledger, _, c := newScheduleService(t)
	ctx := context.Background()
	first := c.t.Add(time.Hour)
	si, err := svc.CreateStanding(ctx, req, domain.FreqDaily, first)
	if err != nil {
		t.Fatal(err)
	}
	for day := 0; day < 3; day++ {
		c.t = first.Add(time.Duration(day)*24*time.Hour + time.Minute)
		if n, err := svc.RunDueSchedules(ctx); err != nil || n != 1 {
			t.Fatalf("day %d: n=%d err=%v", day, n, err)
		}
	}
	if len(ledger.calls) != 3 {
		t.Fatalf("%d payments, want 3", len(ledger.calls))
	}
	seen := map[string]bool{}
	for _, call := range ledger.calls {
		if seen[call.IdempotencyKey] {
			t.Errorf("duplicate idempotency key %s: a later run would be swallowed as a replay", call.IdempotencyKey)
		}
		seen[call.IdempotencyKey] = true
	}
	if !sched.standing[si.ID].NextRunAt.After(c.t) {
		t.Error("next run not advanced into the future")
	}
}

func TestStandingInstructionAfterDowntimeRunsOnceNotABurst(t *testing.T) {
	svc, _, sched, ledger, _, c := newScheduleService(t)
	ctx := context.Background()
	first := c.t.Add(time.Hour)
	si, _ := svc.CreateStanding(ctx, req, domain.FreqDaily, first)
	c.t = first.Add(10 * 24 * time.Hour) // service was down for 10 daily runs
	_, _ = svc.RunDueSchedules(ctx)
	if len(ledger.calls) != 1 {
		t.Errorf("%d back-dated payments fired, want exactly 1", len(ledger.calls))
	}
	if !sched.standing[si.ID].NextRunAt.After(c.t) {
		t.Error("did not resume on the next future date")
	}
}

func TestPauseResumeCancelStanding(t *testing.T) {
	svc, _, sched, ledger, _, c := newScheduleService(t)
	ctx := context.Background()
	first := c.t.Add(time.Hour)
	si, _ := svc.CreateStanding(ctx, req, domain.FreqWeekly, first)

	if err := svc.PauseStanding(ctx, "user-1", si.ID); err != nil {
		t.Fatal(err)
	}
	c.t = first.Add(21 * 24 * time.Hour) // paused through three due dates
	_, _ = svc.RunDueSchedules(ctx)
	if len(ledger.calls) != 0 {
		t.Fatal("a paused instruction ran")
	}

	if err := svc.ResumeStanding(ctx, "user-1", si.ID); err != nil {
		t.Fatal(err)
	}
	if !sched.standing[si.ID].NextRunAt.After(c.t) {
		t.Fatal("resume left next_run in the past, which would fire the missed runs immediately")
	}
	_, _ = svc.RunDueSchedules(ctx)
	if len(ledger.calls) != 0 {
		t.Fatal("resuming fired a missed run")
	}

	if err := svc.CancelStanding(ctx, "user-1", si.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ResumeStanding(ctx, "user-1", si.ID); err == nil {
		t.Error("a cancelled instruction was resumed")
	}
}

func TestStandingValidation(t *testing.T) {
	svc, _, _, _, _, c := newScheduleService(t)
	if _, err := svc.CreateStanding(context.Background(), req, "YEARLY", c.t.Add(time.Hour)); !errors.Is(err, domain.ErrInvalidFrequency) {
		t.Errorf("got %v", err)
	}
}

func TestStandingMonthlyKeepsAnchorDay(t *testing.T) {
	svc, _, sched, _, _, c := newScheduleService(t)
	ctx := context.Background()
	first := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	c.t = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	si, _ := svc.CreateStanding(ctx, req, domain.FreqMonthly, first)
	c.t = first.Add(time.Minute)
	_, _ = svc.RunDueSchedules(ctx) // Aug 31 -> Sep 30 (clamped)
	if got := sched.standing[si.ID].NextRunAt; got.Day() != 30 || got.Month() != time.September {
		t.Fatalf("next run %v, want Sep 30", got)
	}
	c.t = sched.standing[si.ID].NextRunAt.Add(time.Minute)
	_, _ = svc.RunDueSchedules(ctx) // Sep 30 -> Oct 31 (back on the anchor day, not Oct 30)
	if got := sched.standing[si.ID].NextRunAt; got.Day() != 31 || got.Month() != time.October {
		t.Fatalf("next run %v, want Oct 31: the schedule drifted", got)
	}
}
