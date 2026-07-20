package domain

import (
	"math/rand"
	"testing"
)

func TestTransaction_Validate_TableDriven(t *testing.T) {
	cases := []struct {
		name    string
		legs    []Leg
		wantErr error
	}{
		{
			name: "balanced two legs",
			legs: []Leg{
				{AmountMinor: 100_00, Direction: DirectionDebit, Currency: "INR"},
				{AmountMinor: 100_00, Direction: DirectionCredit, Currency: "INR"},
			},
			wantErr: nil,
		},
		{
			name: "balanced split transaction, one debit two credits",
			legs: []Leg{
				{AmountMinor: 100_00, Direction: DirectionDebit, Currency: "INR"},
				{AmountMinor: 60_00, Direction: DirectionCredit, Currency: "INR"},
				{AmountMinor: 40_00, Direction: DirectionCredit, Currency: "INR"},
			},
			wantErr: nil,
		},
		{
			name: "unbalanced",
			legs: []Leg{
				{AmountMinor: 100_00, Direction: DirectionDebit, Currency: "INR"},
				{AmountMinor: 99_00, Direction: DirectionCredit, Currency: "INR"},
			},
			wantErr: ErrUnbalancedLegs,
		},
		{
			name: "balanced per currency independently is not enough — must balance within each currency",
			legs: []Leg{
				{AmountMinor: 100_00, Direction: DirectionDebit, Currency: "INR"},
				{AmountMinor: 100_00, Direction: DirectionCredit, Currency: "USD"},
			},
			wantErr: ErrUnbalancedLegs,
		},
		{
			name: "zero amount rejected",
			legs: []Leg{
				{AmountMinor: 0, Direction: DirectionDebit, Currency: "INR"},
				{AmountMinor: 0, Direction: DirectionCredit, Currency: "INR"},
			},
			wantErr: ErrNonPositiveAmount,
		},
		{
			name: "negative amount rejected",
			legs: []Leg{
				{AmountMinor: -100, Direction: DirectionDebit, Currency: "INR"},
				{AmountMinor: 100, Direction: DirectionCredit, Currency: "INR"},
			},
			wantErr: ErrNonPositiveAmount,
		},
		{
			name:    "single leg rejected",
			legs:    []Leg{{AmountMinor: 100_00, Direction: DirectionDebit, Currency: "INR"}},
			wantErr: ErrTooFewLegs,
		},
		{
			name: "invalid direction rejected",
			legs: []Leg{
				{AmountMinor: 100_00, Direction: "X", Currency: "INR"},
				{AmountMinor: 100_00, Direction: DirectionCredit, Currency: "INR"},
			},
			wantErr: ErrInvalidDirection,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			txn := &Transaction{Legs: tc.legs}
			err := txn.Validate()
			if err != tc.wantErr {
				t.Fatalf("got err %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestTransaction_Validate_Property is the property-based test called for in
// docs/17-testing-strategy.md §2: rather than hand-picked examples, generate
// many random leg sets and assert the invariant holds — "balanced legs
// always validate; legs perturbed to be unbalanced never validate" — across
// a much wider input space than any list of examples could cover. Seeded
// for reproducibility; a failure here should be reproducible by rerunning
// with the same seed.
func TestTransaction_Validate_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(42)) //nolint:gosec // deterministic seed for a reproducible property test, not security-sensitive
	currencies := []string{"INR", "USD"}

	for i := 0; i < 500; i++ {
		currency := currencies[rng.Intn(len(currencies))]
		numLegs := 2 + rng.Intn(6) // 2..7 legs

		// Build a balanced set: numLegs-1 random debits, then one final
		// credit leg (or vice versa) sized to exactly balance the rest —
		// guarantees a valid, non-trivial balanced transaction by construction.
		var total int64
		legs := make([]Leg, 0, numLegs)
		for j := 0; j < numLegs-1; j++ {
			amount := int64(1 + rng.Intn(1_000_000))
			total += amount
			legs = append(legs, Leg{AmountMinor: amount, Direction: DirectionDebit, Currency: currency})
		}
		legs = append(legs, Leg{AmountMinor: total, Direction: DirectionCredit, Currency: currency})

		balanced := &Transaction{Legs: legs}
		if err := balanced.Validate(); err != nil {
			t.Fatalf("iteration %d: balanced transaction rejected: %v (legs=%+v)", i, err, legs)
		}

		// Perturb: bump one random leg's amount by a random positive delta —
		// this must always break the balance and always be rejected.
		perturbed := make([]Leg, len(legs))
		copy(perturbed, legs)
		idx := rng.Intn(len(perturbed))
		delta := int64(1 + rng.Intn(1000))
		perturbed[idx].AmountMinor += delta

		unbalanced := &Transaction{Legs: perturbed}
		if err := unbalanced.Validate(); err != ErrUnbalancedLegs {
			t.Fatalf("iteration %d: perturbed transaction should be rejected as unbalanced, got %v (legs=%+v)", i, err, perturbed)
		}
	}
}
