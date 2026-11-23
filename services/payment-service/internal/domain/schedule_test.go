package domain

import (
	"testing"
	"time"
)

func d(y int, m time.Month, day, h int) time.Time { return time.Date(y, m, day, h, 0, 0, 0, time.UTC) }

func TestNextRunDailyAndWeekly(t *testing.T) {
	if got := NextRun(d(2026, 1, 31, 9), FreqDaily, 31); !got.Equal(d(2026, 2, 1, 9)) {
		t.Errorf("daily: %v", got)
	}
	if got := NextRun(d(2026, 1, 31, 9), FreqWeekly, 31); !got.Equal(d(2026, 2, 7, 9)) {
		t.Errorf("weekly: %v", got)
	}
}

func TestMonthlyClampsToMonthEndWithoutDrifting(t *testing.T) {
	// Starting on the 31st: Jan 31 -> Feb 28 -> Mar 31 (back on the 31st, not stuck on the 28th).
	feb := NextRun(d(2026, 1, 31, 9), FreqMonthly, 31)
	if !feb.Equal(d(2026, 2, 28, 9)) {
		t.Fatalf("Jan 31 -> %v, want Feb 28", feb)
	}
	if mar := NextRun(feb, FreqMonthly, 31); !mar.Equal(d(2026, 3, 31, 9)) {
		t.Errorf("Feb 28 -> %v, want Mar 31", mar)
	}
	if leap := NextRun(d(2028, 1, 31, 9), FreqMonthly, 31); !leap.Equal(d(2028, 2, 29, 9)) {
		t.Errorf("leap year: %v, want Feb 29", leap)
	}
	if dec := NextRun(d(2026, 12, 15, 9), FreqMonthly, 15); !dec.Equal(d(2027, 1, 15, 9)) {
		t.Errorf("year rollover: %v", dec)
	}
}

func TestAdvancePastSkipsMissedRunsInsteadOfBursting(t *testing.T) {
	next := d(2026, 1, 1, 9)
	now := d(2026, 1, 10, 12) // service was down for 9 daily runs
	got := AdvancePast(next, now, FreqDaily, 1)
	if !got.Equal(d(2026, 1, 11, 9)) {
		t.Errorf("got %v, want Jan 11 (first future occurrence)", got)
	}
}

func TestAdvancePastNeverReturnsNowOrEarlier(t *testing.T) {
	now := d(2026, 3, 1, 9)
	if got := AdvancePast(now, now, FreqWeekly, 1); !got.After(now) {
		t.Errorf("got %v, must be strictly after now", got)
	}
}

func TestFrequencyValid(t *testing.T) {
	if !FreqMonthly.Valid() || Frequency("YEARLY").Valid() {
		t.Error("Valid() wrong")
	}
}
