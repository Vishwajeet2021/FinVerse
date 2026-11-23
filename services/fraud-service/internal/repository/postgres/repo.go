package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/finverse/fraud-service/internal/service"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type caseModel struct {
	ID          string     `gorm:"column:id;primaryKey"`
	AccountID   string     `gorm:"column:account_id"`
	PaymentID   *string    `gorm:"column:payment_id"`
	AmountMinor int64      `gorm:"column:amount_minor"`
	Verdict     string     `gorm:"column:verdict"`
	Rule        string     `gorm:"column:rule"`
	Reason      string     `gorm:"column:reason"`
	RiskScore   int        `gorm:"column:risk_score"`
	Status      string     `gorm:"column:status"`
	Resolution  *string    `gorm:"column:resolution"`
	ResolvedBy  *string    `gorm:"column:resolved_by"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	ResolvedAt  *time.Time `gorm:"column:resolved_at"`
}

func (caseModel) TableName() string { return "fraud_cases" }

type outboxModel struct {
	ID          string     `gorm:"column:id;primaryKey"`
	EventType   string     `gorm:"column:event_type"`
	Payload     []byte     `gorm:"column:payload"`
	TraceParent *string    `gorm:"column:traceparent"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	PublishedAt *time.Time `gorm:"column:published_at"`
}

func (outboxModel) TableName() string { return "outbox" }

func (m caseModel) toDomain() service.Case {
	c := service.Case{ID: m.ID, AccountID: m.AccountID, AmountMinor: m.AmountMinor, Verdict: m.Verdict, Rule: m.Rule,
		Reason: m.Reason, RiskScore: m.RiskScore, Status: m.Status, Resolution: m.Resolution, ResolvedBy: m.ResolvedBy,
		CreatedAt: m.CreatedAt, ResolvedAt: m.ResolvedAt}
	if m.PaymentID != nil {
		c.PaymentID = *m.PaymentID
	}
	return c
}

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func outbox(eventType string, payload any, tp *string) (*outboxModel, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &outboxModel{ID: uuid.NewString(), EventType: eventType, Payload: b, TraceParent: tp, CreatedAt: time.Now()}, nil
}

func (r *Repository) OpenCase(ctx context.Context, c service.Case, tp *string) error {
	m := caseModel{ID: uuid.NewString(), AccountID: c.AccountID, AmountMinor: c.AmountMinor, Verdict: c.Verdict,
		Rule: c.Rule, Reason: c.Reason, RiskScore: c.RiskScore, Status: "OPEN", CreatedAt: c.CreatedAt}
	if c.PaymentID != "" {
		m.PaymentID = &c.PaymentID
	}
	ob, err := outbox(service.EventFraudCaseOpened, map[string]any{
		"caseId": m.ID, "accountId": c.AccountID, "paymentId": c.PaymentID, "amountMinor": c.AmountMinor,
		"verdict": c.Verdict, "rule": c.Rule, "riskScore": c.RiskScore}, tp)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&m).Error; err != nil {
			return err
		}
		return tx.Create(ob).Error
	})
}

func (r *Repository) List(ctx context.Context, status string, limit int) ([]service.Case, error) {
	q := r.db.WithContext(ctx).Order("created_at DESC").Limit(limit)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var ms []caseModel
	if err := q.Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]service.Case, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.toDomain())
	}
	return out, nil
}

func (r *Repository) Resolve(ctx context.Context, id, outcome, actor string, tp *string) (service.Case, error) {
	var result service.Case
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m caseModel
		if err := tx.Where("id = ?", id).First(&m).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return service.ErrCaseNotFound
			}
			return err
		}
		if m.Status != "OPEN" {
			return service.ErrCaseNotOpen
		}
		now := time.Now()
		m.Status, m.Resolution, m.ResolvedBy, m.ResolvedAt = "RESOLVED", &outcome, &actor, &now
		if err := tx.Save(&m).Error; err != nil {
			return err
		}
		ob, err := outbox(service.EventFraudCaseResolved, map[string]any{"caseId": id, "outcome": outcome, "actor": actor}, tp)
		if err != nil {
			return err
		}
		result = m.toDomain()
		return tx.Create(ob).Error
	})
	return result, err
}
