package service

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Entry is one ledger leg on the statement account. Direction is "D" or "C"; a credit adds to
// the balance and a debit subtracts, matching how Account Service projects balances.
type Entry struct {
	TransactionID string
	LegID         string
	AmountMinor   int64
	Direction     string
	Currency      string
	At            time.Time
}

var (
	ErrMixedCurrency = errors.New("account history mixes currencies; a single-currency statement cannot be produced")
	ErrBadEntry      = errors.New("ledger returned an entry that cannot be placed on a statement")
)

// FormatMinor renders minor units (paise) as a decimal amount without floating point.
func FormatMinor(minor int64) string {
	neg := minor < 0
	// -minor overflows for MinInt64; amounts that large cannot occur (BIGINT CHECK > 0 plus
	// realistic balances), but format via uint64 so the function is total.
	u := uint64(minor)
	if neg {
		u = -u
	}
	s := fmt.Sprintf("%d.%02d", u/100, u%100)
	if neg {
		return "-" + s
	}
	return s
}

// SafeText neutralises spreadsheet formula injection (docs/23 D-R7): a text cell that begins
// with = + - @ (or a tab/CR, which some programs treat the same way) is prefixed with an
// apostrophe so it is displayed, never evaluated. Only free text goes through this; numbers
// are produced by FormatMinor and must stay numeric.
func SafeText(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

func writeCSV(rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.UseCRLF = true // RFC 4180, and what spreadsheet tools expect
	if err := w.WriteAll(rows); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var statementHeader = []string{"date_utc", "transaction_id", "type", "debit", "credit", "balance", "currency"}

// BuildStatement renders a statement. entries may be in any order; they are sorted into
// chronological order here (time, then leg id) so the running balance is deterministic.
// openingMinor is the balance immediately before from.
func BuildStatement(openingMinor int64, entries []Entry, from, to time.Time) ([]byte, int64, error) {
	cur := ""
	for _, e := range entries {
		if e.AmountMinor <= 0 || (e.Direction != "D" && e.Direction != "C") {
			return nil, 0, ErrBadEntry
		}
		if cur == "" {
			cur = e.Currency
		}
		if e.Currency != cur {
			return nil, 0, ErrMixedCurrency
		}
	}
	if cur == "" {
		cur = "INR"
	}
	sorted := append([]Entry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].At.Equal(sorted[j].At) {
			return sorted[i].At.Before(sorted[j].At)
		}
		return sorted[i].LegID < sorted[j].LegID
	})

	const stamp = "2006-01-02T15:04:05Z"
	rows := [][]string{statementHeader}
	bal := openingMinor
	rows = append(rows, []string{from.UTC().Format(stamp), "", "OPENING_BALANCE", "", "", FormatMinor(bal), cur})
	for _, e := range sorted {
		debit, credit := "", ""
		if e.Direction == "C" {
			bal += e.AmountMinor
			credit = FormatMinor(e.AmountMinor)
		} else {
			bal -= e.AmountMinor
			debit = FormatMinor(e.AmountMinor)
		}
		rows = append(rows, []string{e.At.UTC().Format(stamp), SafeText(e.TransactionID), "TRANSACTION", debit, credit, FormatMinor(bal), cur})
	}
	rows = append(rows, []string{to.UTC().Format(stamp), "", "CLOSING_BALANCE", "", "", FormatMinor(bal), cur})
	out, err := writeCSV(rows)
	return out, bal, err
}

// FraudCase is the subset of a fraud case that the operational report lists.
type FraudCase struct {
	ID          string  `json:"id"`
	AccountID   string  `json:"accountId"`
	PaymentID   string  `json:"paymentId"`
	AmountMinor int64   `json:"amountMinor"`
	Verdict     string  `json:"verdict"`
	Rule        string  `json:"rule"`
	Reason      string  `json:"reason"`
	RiskScore   int     `json:"riskScore"`
	Status      string  `json:"status"`
	Resolution  *string `json:"resolution"`
	CreatedAt   string  `json:"createdAt"`
	ResolvedAt  *string `json:"resolvedAt"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// BuildFraudSummary renders the fraud case report. Every free-text field is injection-guarded.
func BuildFraudSummary(cases []FraudCase) ([]byte, error) {
	rows := [][]string{{"case_id", "account_id", "payment_id", "amount", "verdict", "rule", "reason", "risk_score", "status", "resolution", "created_at", "resolved_at"}}
	for _, c := range cases {
		rows = append(rows, []string{
			SafeText(c.ID), SafeText(c.AccountID), SafeText(c.PaymentID), FormatMinor(c.AmountMinor),
			SafeText(c.Verdict), SafeText(c.Rule), SafeText(strings.TrimSpace(c.Reason)), strconv.Itoa(c.RiskScore),
			SafeText(c.Status), SafeText(deref(c.Resolution)), SafeText(c.CreatedAt), SafeText(deref(c.ResolvedAt)),
		})
	}
	return writeCSV(rows)
}
