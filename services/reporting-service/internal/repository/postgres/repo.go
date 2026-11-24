package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/finverse/reporting-service/internal/service"
	"gorm.io/gorm"
)

type model struct {
	ID          string     `gorm:"column:id;primaryKey"`
	RequesterID string     `gorm:"column:requester_id"`
	Kind        string     `gorm:"column:kind"`
	AccountID   *string    `gorm:"column:account_id"`
	PeriodFrom  *time.Time `gorm:"column:period_from;type:date"`
	PeriodTo    *time.Time `gorm:"column:period_to;type:date"`
	Status      string     `gorm:"column:status"`
	RowCount    int        `gorm:"column:row_count"`
	SizeBytes   int64      `gorm:"column:size_bytes"`
	SHA256      *string    `gorm:"column:sha256"`
	FileKey     *string    `gorm:"column:file_key"`
	FailureCode *string    `gorm:"column:failure_code"`
	Attempts    int        `gorm:"column:attempts"`
	LeaseUntil  *time.Time `gorm:"column:lease_until"`
	Snapshot    []byte     `gorm:"column:snapshot;type:jsonb"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	CompletedAt *time.Time `gorm:"column:completed_at"`
	ExpiresAt   *time.Time `gorm:"column:expires_at"`
}

func (model) TableName() string { return "report_requests" }

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func toDomain(m *model) service.Report {
	return service.Report{
		ID: m.ID, RequesterID: m.RequesterID, Kind: service.Kind(m.Kind), AccountID: str(m.AccountID),
		From: m.PeriodFrom, To: m.PeriodTo, Status: service.Status(m.Status), RowCount: m.RowCount,
		SizeBytes: m.SizeBytes, SHA256: str(m.SHA256), FileKey: str(m.FileKey), FailureCode: str(m.FailureCode),
		Attempts: m.Attempts, Snapshot: m.Snapshot, CreatedAt: m.CreatedAt, CompletedAt: m.CompletedAt, ExpiresAt: m.ExpiresAt,
	}
}

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(ctx context.Context, rep service.Report) error {
	m := model{ID: rep.ID, RequesterID: rep.RequesterID, Kind: string(rep.Kind), Status: string(rep.Status),
		PeriodFrom: rep.From, PeriodTo: rep.To, Snapshot: rep.Snapshot, CreatedAt: rep.CreatedAt}
	if rep.AccountID != "" {
		m.AccountID = &rep.AccountID
	}
	return r.db.WithContext(ctx).Create(&m).Error
}

func (r *Repository) CountActive(ctx context.Context, requesterID string) (int, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model{}).
		Where("requester_id = ? AND status IN ('QUEUED','RUNNING')", requesterID).Count(&n).Error
	return int(n), err
}

func (r *Repository) Get(ctx context.Context, id string) (*service.Report, error) {
	var m model
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, service.ErrNotFound
		}
		return nil, err
	}
	rep := toDomain(&m)
	return &rep, nil
}

func (r *Repository) ListByRequester(ctx context.Context, requesterID string, limit int) ([]service.Report, error) {
	var ms []model
	// The snapshot can be large and the list view never needs it.
	err := r.db.WithContext(ctx).Omit("snapshot").Where("requester_id = ?", requesterID).
		Order("created_at DESC").Limit(limit).Find(&ms).Error
	if err != nil {
		return nil, err
	}
	out := make([]service.Report, len(ms))
	for i := range ms {
		out[i] = toDomain(&ms[i])
	}
	return out, nil
}

// Claim leases the oldest unfinished report. FOR UPDATE SKIP LOCKED lets several replicas
// run workers without ever taking the same report, and a RUNNING report whose lease lapsed
// (its worker died) is picked up again.
func (r *Repository) Claim(ctx context.Context, now time.Time, lease time.Duration) (*service.Report, error) {
	var m model
	res := r.db.WithContext(ctx).Raw(`
		UPDATE report_requests SET status = 'RUNNING', attempts = attempts + 1, lease_until = ?
		WHERE id = (
			SELECT id FROM report_requests
			WHERE status = 'QUEUED' OR (status = 'RUNNING' AND lease_until < ?)
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1)
		RETURNING *`, now.Add(lease), now).Scan(&m)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	rep := toDomain(&m)
	return &rep, nil
}

func (r *Repository) Complete(ctx context.Context, id string, rows int, size int64, sum, fileKey string, now, expires time.Time) error {
	res := r.db.WithContext(ctx).Model(&model{}).Where("id = ? AND status = 'RUNNING'", id).Updates(map[string]any{
		"status": "READY", "row_count": rows, "size_bytes": size, "sha256": sum, "file_key": fileKey,
		"completed_at": now, "expires_at": expires, "lease_until": nil, "failure_code": nil, "snapshot": nil,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return errors.New("report is no longer running (lease lost)")
	}
	return nil
}

func (r *Repository) Fail(ctx context.Context, id, code string, permanent bool, now time.Time) error {
	// attempts was incremented at claim time, so "attempts >= max" means this was the last try.
	return r.db.WithContext(ctx).Exec(`
		UPDATE report_requests SET
		  failure_code = ?,
		  lease_until = NULL,
		  status = CASE WHEN ? OR attempts >= ? THEN 'FAILED' ELSE 'QUEUED' END,
		  completed_at = CASE WHEN ? OR attempts >= ? THEN ?::timestamptz ELSE NULL END
		WHERE id = ? AND status = 'RUNNING'`,
		code, permanent, service.MaxAttempts, permanent, service.MaxAttempts, now, id).Error
}

func (r *Repository) ExpireDue(ctx context.Context, now time.Time) ([]string, error) {
	var keys []string
	err := r.db.WithContext(ctx).Raw(`
		UPDATE report_requests SET status = 'EXPIRED'
		WHERE status = 'READY' AND expires_at < ?
		RETURNING file_key`, now).Scan(&keys).Error
	return keys, err
}
