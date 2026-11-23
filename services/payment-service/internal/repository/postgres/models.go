package postgres

import "time"

type paymentModel struct {
	ID             string     `gorm:"column:id;primaryKey"`
	IdempotencyKey string     `gorm:"column:idempotency_key"`
	FromAccountID  string     `gorm:"column:from_account_id"`
	ToReference    string     `gorm:"column:to_reference"`
	Rail           string     `gorm:"column:rail"`
	AmountMinor    int64      `gorm:"column:amount_minor"`
	Status         string     `gorm:"column:status"`
	TransactionID  *string    `gorm:"column:transaction_id"`
	FailureReason  *string    `gorm:"column:failure_reason"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	SettleAt       *time.Time `gorm:"column:settle_at"`
	ClaimedUntil   *time.Time `gorm:"column:claimed_until"`
	SettleAttempts int        `gorm:"column:settle_attempts"`
}

func (paymentModel) TableName() string { return "payments" }

type outboxModel struct {
	ID          string     `gorm:"column:id;primaryKey"`
	EventType   string     `gorm:"column:event_type"`
	Payload     []byte     `gorm:"column:payload"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	PublishedAt *time.Time `gorm:"column:published_at"`
	TraceParent *string    `gorm:"column:traceparent"`
}

func (outboxModel) TableName() string { return "outbox" }
