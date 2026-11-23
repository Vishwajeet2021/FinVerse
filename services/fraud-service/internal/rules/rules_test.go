package rules

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeCounters struct {
	perMin, daily int64
	err           error
	calls         int
}

func (f *fakeCounters) Record(_ context.Context, _ string, amount int64, _ time.Time) (int64, int64, error) {
	f.calls++
	f.perMin++
	f.daily += amount
	return f.perMin, f.daily, f.err
}

var testCfg = Config{SingleTxnLimitMinor: 5_000_000, ReviewThresholdMinor: 2_500_000, DailyLimitMinor: 10_000_000, VelocityMaxPerMin: 3}

func eval(t *testing.T, c *fakeCounters, amount int64) Decision {
	t.Helper()
	d, err := NewEngine(testCfg, c).Evaluate(context.Background(), Request{AccountID: "a", AmountMinor: amount})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSmallPaymentAllowed(t *testing.T) {
	if d := eval(t, &fakeCounters{}, 10_000); d.Verdict != Allow {
		t.Fatalf("got %+v", d)
	}
}

func TestSingleTxnLimitDeniedWithoutTouchingRedis(t *testing.T) {
	c := &fakeCounters{}
	d := eval(t, c, 5_000_001)
	if d.Verdict != Deny || d.Rule != RuleSingleTxnLimit {
		t.Fatalf("got %+v", d)
	}
	if c.calls != 0 {
		t.Errorf("redis touched %d times for a stateless rejection", c.calls)
	}
}

func TestAtLimitIsAllowedNotDenied(t *testing.T) {
	if d := eval(t, &fakeCounters{}, 5_000_000); d.Verdict == Deny {
		t.Fatalf("exactly at the limit must not be denied: %+v", d)
	}
}

func TestReviewThreshold(t *testing.T) {
	d := eval(t, &fakeCounters{}, 2_500_000)
	if d.Verdict != Review || d.Rule != RuleReviewAmount {
		t.Fatalf("got %+v", d)
	}
}

func TestVelocityDeniesAfterMax(t *testing.T) {
	c := &fakeCounters{}
	for i := 1; i <= 3; i++ {
		if d := eval(t, c, 100); d.Verdict != Allow {
			t.Fatalf("payment %d should be allowed: %+v", i, d)
		}
	}
	if d := eval(t, c, 100); d.Verdict != Deny || d.Rule != RuleVelocity {
		t.Fatalf("4th payment in a minute should be denied: %+v", d)
	}
}

func TestDailyLimit(t *testing.T) {
	c := &fakeCounters{daily: 9_000_000}
	if d := eval(t, c, 1_500_000); d.Verdict != Deny || d.Rule != RuleDailyLimit {
		t.Fatalf("got %+v", d)
	}
}

func TestReasonsNeverLeakThresholds(t *testing.T) {
	for _, d := range []Decision{eval(t, &fakeCounters{}, 9_999_999), eval(t, &fakeCounters{daily: 99_000_000}, 100)} {
		for _, secret := range []string{"5000000", "10000000", "50,000"} {
			if contains(d.Reason, secret) {
				t.Errorf("customer-facing reason leaks %q: %q", secret, d.Reason)
			}
		}
	}
}

func TestCounterFailureSurfacesAsError(t *testing.T) {
	_, err := NewEngine(testCfg, &fakeCounters{err: errors.New("redis down")}).Evaluate(context.Background(), Request{AccountID: "a", AmountMinor: 1})
	if err == nil {
		t.Fatal("expected an error so the caller can apply its fail-open/closed policy")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
