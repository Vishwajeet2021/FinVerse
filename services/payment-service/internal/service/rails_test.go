package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/finverse/payment-service/internal/domain"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newRailService(t *testing.T) (*PaymentService, *fakePaymentRepository, *fakeLedgerClient, *fakeFraudChecker, *clock) {
	t.Helper()
	repo := newFakePaymentRepository()
	ledger := &fakeLedgerClient{}
	fraud := &fakeFraudChecker{}
	c := &clock{t: time.Date(2026, 6, 1, 10, 0, 7, 0, time.UTC)}
	svc := NewPaymentService(repo, ledger, newFakeAccountResolver(), fraud, false)
	svc.now = c.now
	return svc, repo, ledger, fraud, c
}

func TestRailLimits(t *testing.T) {
	c := DefaultRailConfig()
	cases := []struct {
		rail    domain.Rail
		amount  int64
		wantErr bool
	}{
		{domain.RailUPI, 100_000_00, false}, {domain.RailUPI, 100_000_01, true},
		{domain.RailIMPS, 500_000_00, false}, {domain.RailIMPS, 500_000_01, true},
		{domain.RailRTGS, 200_000_00, false}, {domain.RailRTGS, 199_999_99, true},
		{domain.RailNEFT, 1, false}, {domain.RailNEFT, 900_000_000_00, false},
		{domain.RailInternal, 1, false},
		{domain.Rail("SWIFT"), 100, true},
	}
	for _, tc := range cases {
		err := c.Validate(tc.rail, tc.amount)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s %d: err=%v, wantErr=%v", tc.rail, tc.amount, err, tc.wantErr)
		}
	}
	if err := c.Validate(domain.RailUPI, 100_000_01); !errors.Is(err, domain.ErrRailLimit) {
		t.Errorf("want ErrRailLimit, got %v", err)
	}
	if err := c.Validate("SWIFT", 1); !errors.Is(err, domain.ErrUnknownRail) {
		t.Errorf("want ErrUnknownRail, got %v", err)
	}
}

func TestSettleAtPerRail(t *testing.T) {
	c := RailConfig{NeftBatch: 30 * time.Second}
	now := time.Date(2026, 6, 1, 10, 0, 7, 0, time.UTC)
	if got := c.SettleAt(domain.RailUPI, now); got.Sub(now) != 2*time.Second {
		t.Errorf("UPI settles in %v", got.Sub(now))
	}
	if got := c.SettleAt(domain.RailRTGS, now); got.Sub(now) != 5*time.Second {
		t.Errorf("RTGS settles in %v", got.Sub(now))
	}
	// NEFT waits for the next batch boundary: 10:00:07 -> 10:00:30; exactly on a boundary -> the *next* one.
	if got := c.SettleAt(domain.RailNEFT, now); !got.Equal(time.Date(2026, 6, 1, 10, 0, 30, 0, time.UTC)) {
		t.Errorf("NEFT at %v", got)
	}
	onBoundary := time.Date(2026, 6, 1, 10, 0, 30, 0, time.UTC)
	if got := c.SettleAt(domain.RailNEFT, onBoundary); !got.Equal(onBoundary.Add(30 * time.Second)) {
		t.Errorf("NEFT on boundary -> %v", got)
	}
}

func TestAsyncPaymentStopsAtProcessingAndMovesNoMoney(t *testing.T) {
	svc, repo, ledger, _, _ := newRailService(t)
	p, err := svc.InitiatePaymentOnRail(context.Background(), domain.RailUPI, "acct-1", "acct-2", 1_000, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != domain.StatusProcessing || p.SettleAt == nil || p.Rail != domain.RailUPI {
		t.Fatalf("got %+v", p)
	}
	if len(ledger.calls) != 0 {
		t.Error("ledger posted before settlement")
	}
	if repo.byID[p.ID].Status != domain.StatusProcessing {
		t.Error("stored status not PROCESSING")
	}
}

func TestAsyncRailLimitRejectedBeforeAnyRecord(t *testing.T) {
	svc, repo, _, fraud, _ := newRailService(t)
	_, err := svc.InitiatePaymentOnRail(context.Background(), domain.RailRTGS, "acct-1", "acct-2", 1_000, "k2")
	if !errors.Is(err, domain.ErrRailLimit) {
		t.Fatalf("got %v", err)
	}
	if len(repo.byID) != 0 || fraud.calls != 0 {
		t.Error("a request that violates rail limits must leave no trace and cost no fraud check")
	}
}

func TestAsyncPaymentStillFraudScreened(t *testing.T) {
	svc, _, ledger, fraud, _ := newRailService(t)
	fraud.verdict = FraudVerdict{Decision: FraudDeny, Reason: "no"}
	p, _ := svc.InitiatePaymentOnRail(context.Background(), domain.RailIMPS, "acct-1", "acct-2", 1_000, "k3")
	if p.Status != domain.StatusFailed || len(ledger.calls) != 0 {
		t.Fatalf("denied async payment: %+v, ledger calls %d", p, len(ledger.calls))
	}
}

func TestSettleDueWaitsForSettleTimeThenCompletes(t *testing.T) {
	svc, repo, ledger, _, c := newRailService(t)
	p, _ := svc.InitiatePaymentOnRail(context.Background(), domain.RailUPI, "acct-1", "acct-2", 1_000, "k4")

	if n, _ := svc.SettleDue(context.Background()); n != 0 || len(ledger.calls) != 0 {
		t.Fatalf("settled %d before the rail cleared", n)
	}
	c.t = c.t.Add(3 * time.Second)
	n, err := svc.SettleDue(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if repo.byID[p.ID].Status != domain.StatusCompleted {
		t.Errorf("status %s", repo.byID[p.ID].Status)
	}
	if len(ledger.calls) != 1 || ledger.calls[0].IdempotencyKey != "k4" {
		t.Errorf("ledger calls %+v: must post once with the payment's idempotency key", ledger.calls)
	}
	// A second pass must not post again.
	c.t = c.t.Add(time.Minute)
	_, _ = svc.SettleDue(context.Background())
	if len(ledger.calls) != 1 {
		t.Errorf("settled twice: %d ledger calls", len(ledger.calls))
	}
}

func TestSettleDueSimulatedRailRejection(t *testing.T) {
	svc, repo, ledger, _, c := newRailService(t)
	svc.WithRails(RailConfig{NeftBatch: 30 * time.Second, FailureRate: 1})
	svc.rng = func() float64 { return 0 } // always below the failure rate
	p, _ := svc.InitiatePaymentOnRail(context.Background(), domain.RailIMPS, "acct-1", "acct-2", 1_000, "k5")
	c.t = c.t.Add(10 * time.Second)
	_, _ = svc.SettleDue(context.Background())

	got := repo.byID[p.ID]
	if got.Status != domain.StatusFailed || got.FailureReason == nil || *got.FailureReason == "" {
		t.Fatalf("got %+v", got)
	}
	if len(ledger.calls) != 0 {
		t.Error("a rejected payment must not touch the ledger")
	}
	last := repo.outboxEvents[len(repo.outboxEvents)-1]
	if last.EventType != EventPaymentFailed {
		t.Errorf("last event %s", last.EventType)
	}
}

func TestSettleDueRetriesTransientLedgerErrorWithBackoffThenSucceeds(t *testing.T) {
	svc, repo, ledger, _, c := newRailService(t)
	p, _ := svc.InitiatePaymentOnRail(context.Background(), domain.RailUPI, "acct-1", "acct-2", 1_000, "k6")
	c.t = c.t.Add(3 * time.Second)

	ledger.err = errors.New("transaction-service unavailable")
	if n, _ := svc.SettleDue(context.Background()); n != 0 {
		t.Fatal("a transient failure must not be terminal")
	}
	if repo.byID[p.ID].Status != domain.StatusProcessing {
		t.Fatalf("status %s, want still PROCESSING", repo.byID[p.ID].Status)
	}
	// Still inside the 2s backoff: not retried yet.
	calls := len(ledger.calls)
	_, _ = svc.SettleDue(context.Background())
	if len(ledger.calls) != calls {
		t.Error("retried inside the backoff window")
	}
	// Dependency recovers; after the backoff the retry succeeds.
	ledger.err = nil
	c.t = c.t.Add(3 * time.Second)
	if n, _ := svc.SettleDue(context.Background()); n != 1 || repo.byID[p.ID].Status != domain.StatusCompleted {
		t.Fatalf("did not recover: n=%d status=%s", n, repo.byID[p.ID].Status)
	}
}

func TestSettleDueGivesUpAfterMaxAttempts(t *testing.T) {
	svc, repo, ledger, _, c := newRailService(t)
	p, _ := svc.InitiatePaymentOnRail(context.Background(), domain.RailUPI, "acct-1", "acct-2", 1_000, "k7")
	ledger.err = errors.New("down for good")
	c.t = c.t.Add(3 * time.Second)
	for i := 0; i < 10 && repo.byID[p.ID].Status == domain.StatusProcessing; i++ {
		_, _ = svc.SettleDue(context.Background())
		c.t = c.t.Add(time.Minute)
	}
	got := repo.byID[p.ID]
	if got.Status != domain.StatusFailed || got.FailureReason == nil {
		t.Fatalf("got %+v", got)
	}
	if len(ledger.calls) != maxSettleAttempts {
		t.Errorf("ledger tried %d times, want exactly %d", len(ledger.calls), maxSettleAttempts)
	}
}

func TestSettleDueRecipientGoneFailsImmediately(t *testing.T) {
	svc, repo, ledger, _, c := newRailService(t)
	resolver := newFakeAccountResolver()
	svc.account = resolver
	p, _ := svc.InitiatePaymentOnRail(context.Background(), domain.RailUPI, "acct-1", "acct-2", 1_000, "k8")
	resolver.missing["acct-2"] = true // account closed between initiation and settlement
	c.t = c.t.Add(3 * time.Second)
	_, _ = svc.SettleDue(context.Background())
	if repo.byID[p.ID].Status != domain.StatusFailed || len(ledger.calls) != 0 {
		t.Fatalf("status %s, ledger calls %d", repo.byID[p.ID].Status, len(ledger.calls))
	}
}

func TestFrozenAccountCannotInitiateAnyPayment(t *testing.T) {
	svc, repo, ledger, fraud, _ := newRailService(t)
	resolver := newFakeAccountResolver()
	resolver.debit["acct-1"] = domain.ErrSourceAccountFrozen
	svc.account = resolver

	for rail, amount := range map[domain.Rail]int64{domain.RailInternal: 100, domain.RailUPI: 100, domain.RailRTGS: 200_000_00} {
		_, err := svc.InitiatePaymentOnRail(context.Background(), rail, "acct-1", "acct-2", amount, "k-"+string(rail))
		if !errors.Is(err, domain.ErrSourceAccountFrozen) {
			t.Errorf("%s: err = %v, want ErrSourceAccountFrozen", rail, err)
		}
	}
	if len(repo.byID) != 0 || len(ledger.calls) != 0 || fraud.calls != 0 {
		t.Errorf("a refused payment must leave nothing behind: payments=%d ledger=%d fraud=%d", len(repo.byID), len(ledger.calls), fraud.calls)
	}
}

func TestClosedAccountCannotPay(t *testing.T) {
	svc, _, _, _, _ := newRailService(t)
	resolver := newFakeAccountResolver()
	resolver.debit["acct-1"] = domain.ErrSourceAccountClosed
	svc.account = resolver
	if _, err := svc.InitiatePayment(context.Background(), "acct-1", "acct-2", 100, "k"); !errors.Is(err, domain.ErrSourceAccountClosed) {
		t.Errorf("err = %v", err)
	}
}

func TestUnknownAccountStateFailsClosed(t *testing.T) {
	svc, repo, ledger, _, _ := newRailService(t)
	resolver := newFakeAccountResolver()
	resolver.debit["acct-1"] = errors.New("account-service unreachable")
	svc.account = resolver
	if _, err := svc.InitiatePayment(context.Background(), "acct-1", "acct-2", 100, "k"); err == nil {
		t.Fatal("a payment was accepted although the account state could not be checked")
	}
	if len(repo.byID) != 0 || len(ledger.calls) != 0 {
		t.Error("nothing may be recorded or posted when the account state is unknown")
	}
}

func TestFreezeAfterAcceptanceStopsSettlement(t *testing.T) {
	svc, repo, ledger, _, c := newRailService(t)
	resolver := newFakeAccountResolver()
	svc.account = resolver
	p, err := svc.InitiatePaymentOnRail(context.Background(), domain.RailUPI, "acct-1", "acct-2", 1_000, "k-late")
	if err != nil {
		t.Fatal(err)
	}
	resolver.debit["acct-1"] = domain.ErrSourceAccountFrozen // frozen while the payment is clearing
	c.t = c.t.Add(3 * time.Second)
	_, _ = svc.SettleDue(context.Background())
	got := repo.byID[p.ID]
	if got.Status != domain.StatusFailed || got.FailureReason == nil || !strings.HasPrefix(*got.FailureReason, "ACCOUNT_FROZEN") {
		t.Fatalf("payment = %+v", got)
	}
	if len(ledger.calls) != 0 {
		t.Error("money moved from a frozen account")
	}
}

func TestUnknownStateAtSettlementRetriesInsteadOfPosting(t *testing.T) {
	svc, repo, ledger, _, c := newRailService(t)
	resolver := newFakeAccountResolver()
	svc.account = resolver
	p, _ := svc.InitiatePaymentOnRail(context.Background(), domain.RailUPI, "acct-1", "acct-2", 1_000, "k-unknown")
	resolver.debit["acct-1"] = errors.New("account-service unreachable")
	c.t = c.t.Add(3 * time.Second)
	_, _ = svc.SettleDue(context.Background())
	if repo.byID[p.ID].Status == domain.StatusCompleted || len(ledger.calls) != 0 {
		t.Fatalf("settled blind: status=%s ledger calls=%d", repo.byID[p.ID].Status, len(ledger.calls))
	}
}
