package postgres

import "time"

type userModel struct {
	ID               string     `gorm:"column:id;primaryKey"`
	Email            string     `gorm:"column:email"`
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
}

func (userModel) TableName() string { return "users" }

type refreshTokenModel struct {
	ID        string    `gorm:"column:id;primaryKey"`
	UserID    string    `gorm:"column:user_id"`
	TokenHash string    `gorm:"column:token_hash"`
	FamilyID  string    `gorm:"column:family_id"`
	Revoked   bool      `gorm:"column:revoked"`
	ExpiresAt time.Time `gorm:"column:expires_at"`
	CreatedAt time.Time `gorm:"column:created_at"`
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
