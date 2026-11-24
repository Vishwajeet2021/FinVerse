package domain

import "time"

// ChallengePurpose is what a pending sign-in step is for.
type ChallengePurpose string

const (
	// ChallengeLogin: the user has a second factor and must present a code to finish signing in.
	ChallengeLogin ChallengePurpose = "LOGIN"
	// ChallengeEnroll: the user's role requires a second factor and none is set up yet. The
	// challenge can do exactly one thing, enrol one, and finishing enrolment finishes the sign-in.
	ChallengeEnroll ChallengePurpose = "ENROLL"
)

const (
	// MFAChallengeTTL bounds how long someone may sit on a correct password before the second step.
	MFAChallengeTTL = 5 * time.Minute
	// MFAChallengeMaxAttempts bounds guesses against one challenge. Each wrong code also counts
	// towards the account lockout (FR-AUTH-9), so opening new challenges does not reset the budget.
	MFAChallengeMaxAttempts = 5
)

// MFAChallenge is the intermediate state between "password accepted" and "session issued".
type MFAChallenge struct {
	ID         string
	UserID     string
	TokenHash  string
	Purpose    ChallengePurpose
	Attempts   int
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedAt  time.Time
}
