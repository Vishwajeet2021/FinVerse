package service

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFormatMinor(t *testing.T) {
	for in, want := range map[int64]string{0: "0.00", 5: "0.05", 100: "1.00", 123456: "1234.56", -5: "-0.05", -250075: "-2500.75"} {
		if got := FormatMinor(in); got != want {
			t.Errorf("FormatMinor(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestSafeTextNeutralisesFormulas(t *testing.T) {
	for _, bad := range []string{"=SUM(A1)", "+1+1", "-2+3", "@cmd", "\tcmd", "\rcmd"} {
		if got := SafeText(bad); got != "'"+bad {
			t.Errorf("SafeText(%q) = %q", bad, got)
		}
	}
	for _, ok := range []string{"", "Rent", "a=b", "100"} {
		if SafeText(ok) != ok {
			t.Errorf("SafeText(%q) must not change", ok)
		}
	}
}

var (
	jan1  = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	jan31 = time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC)
)

func TestBuildStatementRunningBalanceAndOrdering(t *testing.T) {
	entries := []Entry{ // deliberately newest first, as the ledger returns them
		{TransactionID: "t3", LegID: "l3", AmountMinor: 2500, Direction: "D", Currency: "INR", At: jan1.Add(72 * time.Hour)},
		{TransactionID: "t2", LegID: "l2", AmountMinor: 10000, Direction: "C", Currency: "INR", At: jan1.Add(48 * time.Hour)},
		{TransactionID: "t1", LegID: "l1", AmountMinor: 1, Direction: "C", Currency: "INR", At: jan1.Add(24 * time.Hour)},
	}
	out, closing, err := BuildStatement(5000, entries, jan1, jan31)
	if err != nil {
		t.Fatal(err)
	}
	if closing != 5000+1+10000-2500 {
		t.Errorf("closing = %d", closing)
	}
	want := strings.Join([]string{
		"date_utc,transaction_id,type,debit,credit,balance,currency",
		"2026-01-01T00:00:00Z,,OPENING_BALANCE,,,50.00,INR",
		"2026-01-02T00:00:00Z,t1,TRANSACTION,,0.01,50.01,INR",
		"2026-01-03T00:00:00Z,t2,TRANSACTION,,100.00,150.01,INR",
		"2026-01-04T00:00:00Z,t3,TRANSACTION,25.00,,125.01,INR",
		"2026-01-31T23:59:59Z,,CLOSING_BALANCE,,,125.01,INR",
		"",
	}, "\r\n")
	if string(out) != want {
		t.Errorf("csv mismatch:\n got: %q\nwant: %q", out, want)
	}
}

func TestBuildStatementEmptyRangeStillHasBothBalances(t *testing.T) {
	out, closing, err := BuildStatement(777, nil, jan1, jan31)
	if err != nil || closing != 777 {
		t.Fatalf("closing=%d err=%v", closing, err)
	}
	if n := strings.Count(string(out), "\r\n"); n != 3 {
		t.Errorf("want header + opening + closing, got %d lines", n)
	}
}

func TestBuildStatementOverdrawnBalanceStaysNumeric(t *testing.T) {
	out, _, _ := BuildStatement(0, []Entry{{TransactionID: "t", LegID: "l", AmountMinor: 500, Direction: "D", Currency: "INR", At: jan1}}, jan1, jan31)
	if !strings.Contains(string(out), ",-5.00,INR") {
		t.Errorf("a negative balance must be a plain number, not guarded text: %q", out)
	}
}

func TestBuildStatementSameInstantIsDeterministic(t *testing.T) {
	a := Entry{TransactionID: "ta", LegID: "a", AmountMinor: 1, Direction: "C", Currency: "INR", At: jan1}
	b := Entry{TransactionID: "tb", LegID: "b", AmountMinor: 2, Direction: "C", Currency: "INR", At: jan1}
	x, _, _ := BuildStatement(0, []Entry{a, b}, jan1, jan31)
	y, _, _ := BuildStatement(0, []Entry{b, a}, jan1, jan31)
	if string(x) != string(y) {
		t.Error("output depends on input order")
	}
}

func TestBuildStatementRejectsBadLedgerData(t *testing.T) {
	good := Entry{TransactionID: "t", LegID: "l", AmountMinor: 1, Direction: "C", Currency: "INR", At: jan1}
	for name, e := range map[string]Entry{
		"zero amount": {AmountMinor: 0, Direction: "C", Currency: "INR"},
		"negative":    {AmountMinor: -1, Direction: "C", Currency: "INR"},
		"direction":   {AmountMinor: 1, Direction: "X", Currency: "INR"},
	} {
		if _, _, err := BuildStatement(0, []Entry{e}, jan1, jan31); !errors.Is(err, ErrBadEntry) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	usd := good
	usd.Currency = "USD"
	if _, _, err := BuildStatement(0, []Entry{good, usd}, jan1, jan31); !errors.Is(err, ErrMixedCurrency) {
		t.Errorf("mixed currencies: err = %v", err)
	}
}

func TestFraudSummaryGuardsFreeText(t *testing.T) {
	res := "CONFIRMED"
	out, err := BuildFraudSummary([]FraudCase{{
		ID: "c1", AccountID: "a1", AmountMinor: 150000, Verdict: "REVIEW", Rule: "REVIEW_AMOUNT",
		Reason: `=HYPERLINK("http://evil","x"), with comma`, RiskScore: 60, Status: "RESOLVED", Resolution: &res, CreatedAt: "2026-01-01T00:00:00Z",
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"'=HYPERLINK(""http://evil"",""x""), with comma"`) {
		t.Errorf("reason not guarded and quoted: %s", s)
	}
	if !strings.Contains(s, ",1500.00,") {
		t.Errorf("amount not rendered: %s", s)
	}
}
