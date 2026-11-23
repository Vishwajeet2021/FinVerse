package domain

import (
	"errors"
	"time"
)

var (
	ErrNotAccountHolder   = errors.New("you do not hold that account")
	ErrRailLimit          = errors.New("amount is outside the limits of that payment rail")
	ErrUnknownRail        = errors.New("unknown payment rail")
	ErrScheduleInPast     = errors.New("a scheduled payment must be in the future")
	ErrInvalidFrequency   = errors.New("frequency must be DAILY, WEEKLY or MONTHLY")
	ErrScheduleNotFound   = errors.New("schedule not found")
	ErrScheduleNotPending = errors.New("schedule is no longer active")
)

type Frequency string

const (
	FreqDaily   Frequency = "DAILY"
	FreqWeekly  Frequency = "WEEKLY"
	FreqMonthly Frequency = "MONTHLY"
)

func (f Frequency) Valid() bool { return f == FreqDaily || f == FreqWeekly || f == FreqMonthly }

type ScheduledPayment struct {
	ID            string
	CreatedBy     string
	FromAccountID string
	ToReference   string
	Rail          Rail
	AmountMinor   int64
	RunAt         time.Time
	Status        string // PENDING | EXECUTED | CANCELLED
	PaymentID     *string
	CreatedAt     time.Time
}

type StandingInstruction struct {
	ID            string
	CreatedBy     string
	FromAccountID string
	ToReference   string
	Rail          Rail
	AmountMinor   int64
	Frequency     Frequency
	NextRunAt     time.Time
	AnchorDay     int
	Status        string // ACTIVE | PAUSED | CANCELLED
	LastPaymentID *string
	CreatedAt     time.Time
}

// NextRun returns the occurrence after t for the frequency. Monthly keeps
// the original day-of-month where it exists and clamps to the month's last
// day otherwise (31 Jan -> 28/29 Feb), rather than Go's AddDate overflow
// into March, which would drift the schedule permanently.
func NextRun(t time.Time, f Frequency, anchorDay int) time.Time {
	switch f {
	case FreqDaily:
		return t.AddDate(0, 0, 1)
	case FreqWeekly:
		return t.AddDate(0, 0, 7)
	default:
		y, m, _ := t.Date()
		first := time.Date(y, m+1, 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
		last := first.AddDate(0, 1, -1).Day()
		day := anchorDay
		if day > last {
			day = last
		}
		return time.Date(first.Year(), first.Month(), day, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	}
}

// AdvancePast moves next forward by whole periods until it is strictly after
// now. A standing instruction that missed several runs (service down) executes
// once and then resumes on the next future date: it never fires a burst of
// back-dated payments. anchorDay is the day-of-month of the original schedule.
func AdvancePast(next, now time.Time, f Frequency, anchorDay int) time.Time {
	for !next.After(now) {
		next = NextRun(next, f, anchorDay)
	}
	return next
}
