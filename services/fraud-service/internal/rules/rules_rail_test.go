package rules

import (
	"context"
	"testing"
)

func evalRail(t *testing.T, cfg Config, rail string, amount int64) Decision {
	t.Helper()
	d, err := NewEngine(cfg, &fakeCounters{}).Evaluate(context.Background(), Request{AccountID: "a", AmountMinor: amount, Rail: rail})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestRTGSUsesItsOwnSingleLimit(t *testing.T) {
	cfg := Config{SingleTxnLimitMinor: 5_000_000, RTGSSingleTxnLimitMinor: 100_000_000}
	if d := evalRail(t, cfg, "RTGS", 20_000_000); d.Verdict != Allow {
		t.Fatalf("RTGS 2L should pass the RTGS cap, got %+v", d)
	}
	if d := evalRail(t, cfg, "UPI", 20_000_000); d.Verdict != Deny {
		t.Fatalf("non-RTGS keeps the retail cap, got %+v", d)
	}
	if d := evalRail(t, cfg, "RTGS", 100_000_001); d.Verdict != Deny || d.Rule != RuleSingleTxnLimit {
		t.Fatalf("RTGS above its own cap must be denied, got %+v", d)
	}
}

func TestRTGSFallsBackToRetailLimitWhenUnset(t *testing.T) {
	cfg := Config{SingleTxnLimitMinor: 5_000_000}
	if d := evalRail(t, cfg, "RTGS", 20_000_000); d.Verdict != Deny {
		t.Fatalf("unset RTGS cap falls back to retail, got %+v", d)
	}
}
