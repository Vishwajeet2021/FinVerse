package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/finverse/auth-service/internal/domain"
	"gorm.io/gorm"
)

// MFARepository owns every write to the second-factor columns. Each mutation is a single
// conditional statement (or a short transaction), so the guarantees hold under concurrency
// rather than depending on the order of a read-then-write in the service.
type MFARepository struct {
	db *gorm.DB
}

func NewMFARepository(db *gorm.DB) *MFARepository { return &MFARepository{db: db} }

// SetPendingSecret stores an encrypted secret for an enrolment in progress. It refuses once MFA
// is enabled, so an authenticated session (or a stolen challenge) cannot replace a live secret.
func (r *MFARepository) SetPendingSecret(ctx context.Context, userID, secretEnc string) error {
	res := r.db.WithContext(ctx).Exec(
		`UPDATE users SET mfa_secret_enc = ?, updated_at = now() WHERE id = ? AND mfa_enabled = FALSE`,
		secretEnc, userID)
	if res.Error != nil {
		return fmt.Errorf("storing pending MFA secret: %w", res.Error)
	}
	if res.RowsAffected != 1 {
		return domain.ErrMFAAlreadyEnabled
	}
	return nil
}

// Activate turns MFA on and stores the recovery-code hashes, atomically. acceptedStep is the TOTP
// step the confirming code was for, recorded so that exact code cannot be reused straight away.
// It fails with ErrMFAAlreadyEnabled if two confirmations race: only one can win.
func (r *MFARepository) Activate(ctx context.Context, userID string, acceptedStep int64, recoveryHashes []string, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(
			`UPDATE users SET mfa_enabled = TRUE, mfa_enrolled_at = ?, mfa_last_step = ?, updated_at = ?
			   WHERE id = ? AND mfa_enabled = FALSE AND mfa_secret_enc IS NOT NULL`,
			now, acceptedStep, now, userID)
		if res.Error != nil {
			return fmt.Errorf("enabling MFA: %w", res.Error)
		}
		if res.RowsAffected != 1 {
			return domain.ErrMFAAlreadyEnabled
		}
		if err := tx.Exec(`DELETE FROM mfa_recovery_codes WHERE user_id = ?`, userID).Error; err != nil {
			return fmt.Errorf("clearing old recovery codes: %w", err)
		}
		for _, h := range recoveryHashes {
			if err := tx.Exec(
				`INSERT INTO mfa_recovery_codes (user_id, code_hash) VALUES (?, ?)`, userID, h).Error; err != nil {
				return fmt.Errorf("storing recovery code: %w", err)
			}
		}
		return nil
	})
}

// AdvanceStep records step as the newest accepted TOTP step, and reports whether it was newer
// than the last one. A false result means this step (or a later one) was already used: a replay.
func (r *MFARepository) AdvanceStep(ctx context.Context, userID string, step int64) (bool, error) {
	res := r.db.WithContext(ctx).Exec(
		`UPDATE users SET mfa_last_step = ? WHERE id = ? AND mfa_enabled = TRUE AND mfa_last_step < ?`,
		step, userID, step)
	if res.Error != nil {
		return false, fmt.Errorf("advancing MFA step: %w", res.Error)
	}
	return res.RowsAffected == 1, nil
}

// ConsumeRecoveryCode marks a code used. It returns false if there is no such unused code; the
// single UPDATE makes "use once" hold even if the same code is submitted twice at the same time.
func (r *MFARepository) ConsumeRecoveryCode(ctx context.Context, userID, codeHash string) (bool, error) {
	res := r.db.WithContext(ctx).Exec(
		`UPDATE mfa_recovery_codes SET used_at = now()
		   WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`, userID, codeHash)
	if res.Error != nil {
		return false, fmt.Errorf("consuming recovery code: %w", res.Error)
	}
	return res.RowsAffected == 1, nil
}

func (r *MFARepository) CreateChallenge(ctx context.Context, c *domain.MFAChallenge) error {
	m := &mfaChallengeModel{
		ID: c.ID, UserID: c.UserID, TokenHash: c.TokenHash, Purpose: string(c.Purpose),
		Attempts: c.Attempts, ExpiresAt: c.ExpiresAt, ConsumedAt: c.ConsumedAt, CreatedAt: c.CreatedAt,
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("storing MFA challenge: %w", err)
	}
	return nil
}

func (r *MFARepository) GetChallengeByHash(ctx context.Context, tokenHash string) (*domain.MFAChallenge, error) {
	var m mfaChallengeModel
	if err := r.db.WithContext(ctx).First(&m, "token_hash = ?", tokenHash).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrMFAChallengeInvalid
		}
		return nil, fmt.Errorf("querying MFA challenge: %w", err)
	}
	return &domain.MFAChallenge{
		ID: m.ID, UserID: m.UserID, TokenHash: m.TokenHash, Purpose: domain.ChallengePurpose(m.Purpose),
		Attempts: m.Attempts, ExpiresAt: m.ExpiresAt, ConsumedAt: m.ConsumedAt, CreatedAt: m.CreatedAt,
	}, nil
}

// RecordChallengeAttempt counts one guess and returns the new total. Counting happens in the
// database, so parallel guesses cannot all read the same "attempts = 0" and each get a free try.
func (r *MFARepository) RecordChallengeAttempt(ctx context.Context, id string) (int, error) {
	var attempts int
	err := r.db.WithContext(ctx).Raw(
		`UPDATE mfa_challenges SET attempts = attempts + 1 WHERE id = ? RETURNING attempts`, id).Scan(&attempts).Error
	if err != nil {
		return 0, fmt.Errorf("recording MFA attempt: %w", err)
	}
	return attempts, nil
}

// ConsumeChallenge marks the challenge used; false means someone else already consumed it.
func (r *MFARepository) ConsumeChallenge(ctx context.Context, id string) (bool, error) {
	res := r.db.WithContext(ctx).Exec(
		`UPDATE mfa_challenges SET consumed_at = now() WHERE id = ? AND consumed_at IS NULL`, id)
	if res.Error != nil {
		return false, fmt.Errorf("consuming MFA challenge: %w", res.Error)
	}
	return res.RowsAffected == 1, nil
}
