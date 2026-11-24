package postgres

import "time"

type userModel struct {
	ID    string `gorm:"column:id;primaryKey"`
	Email string `gorm:"column:email"`
	// Phone is a pointer so an omitted phone number stores SQL NULL, not "" —
	// the column is UNIQUE, and Postgres treats NULL <> NULL (any number of
	// NULLs are allowed) but "" = "" (a second empty-string phone number
	// would collide with the first user who omitted theirs).
	Phone            *string    `gorm:"column:phone"`
	PasswordHash     string     `gorm:"column:password_hash"`
	Role             string     `gorm:"column:role"`
	EmailVerified    bool       `gorm:"column:email_verified"`
	Status           string     `gorm:"column:status"`
	FailedLoginCount int        `gorm:"column:failed_login_count"`
	LockedUntil      *time.Time `gorm:"column:locked_until"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`

	// The "->" tag makes these read-only for GORM: Save() (used by UserRepository.Update, which
	// writes the whole row) never writes them. Only mfa_repo.go changes these columns, with
	// conditional statements, so a stale user struct cannot roll back the replay counter.
	MFASecretEnc  *string    `gorm:"column:mfa_secret_enc;->"`
	MFAEnabled    bool       `gorm:"column:mfa_enabled;->"`
	MFALastStep   int64      `gorm:"column:mfa_last_step;->"`
	MFAEnrolledAt *time.Time `gorm:"column:mfa_enrolled_at;->"`
}

func (userModel) TableName() string { return "users" }

type refreshTokenModel struct {
	ID        string `gorm:"column:id;primaryKey"`
	UserID    string `gorm:"column:user_id"`
	TokenHash string `gorm:"column:token_hash"`
	FamilyID  string `gorm:"column:family_id"`
	Revoked   bool   `gorm:"column:revoked"`
	// MFAVerified is carried through rotation; see domain.RefreshToken.
	MFAVerified bool      `gorm:"column:mfa_verified"`
	ExpiresAt   time.Time `gorm:"column:expires_at"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

func (refreshTokenModel) TableName() string { return "refresh_tokens" }

// verificationTokenModel backs both email_verification_tokens and
// password_reset_tokens — same shape, different tables
// (docs/08-database-design.md §7) — the table name is injected per repository
// instance rather than hardcoded (see verification_token_repo.go).
type verificationTokenModel struct {
	ID        string     `gorm:"column:id;primaryKey"`
	UserID    string     `gorm:"column:user_id"`
	TokenHash string     `gorm:"column:token_hash"`
	ExpiresAt time.Time  `gorm:"column:expires_at"`
	UsedAt    *time.Time `gorm:"column:used_at"`
	CreatedAt time.Time  `gorm:"column:created_at"`
}

type mfaChallengeModel struct {
	ID         string     `gorm:"column:id;primaryKey"`
	UserID     string     `gorm:"column:user_id"`
	TokenHash  string     `gorm:"column:token_hash"`
	Purpose    string     `gorm:"column:purpose"`
	Attempts   int        `gorm:"column:attempts"`
	ExpiresAt  time.Time  `gorm:"column:expires_at"`
	ConsumedAt *time.Time `gorm:"column:consumed_at"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
}

func (mfaChallengeModel) TableName() string { return "mfa_challenges" }
