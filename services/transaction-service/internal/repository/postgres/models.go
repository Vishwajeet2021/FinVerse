package postgres

import "time"

type transactionModel struct {
	ID                    string    `gorm:"column:id;primaryKey"`
	IdempotencyKey        string    `gorm:"column:idempotency_key"`
	Status                string    `gorm:"column:status"`
	ReversesTransactionID *string   `gorm:"column:reverses_transaction_id"`
	SourceService         string    `gorm:"column:source_service"`
	CreatedAt             time.Time `gorm:"column:created_at"`
}

func (transactionModel) TableName() string { return "transactions" }

type legModel struct {
	ID              string `gorm:"column:id;primaryKey"`
	TransactionID   string `gorm:"column:transaction_id"`
	ParticipantID   string `gorm:"column:participant_id"`
	ParticipantType string `gorm:"column:participant_type"`
	AmountMinor     int64  `gorm:"column:amount_minor"`
	Direction       string `gorm:"column:direction"`
	Currency        string `gorm:"column:currency"`
}

func (legModel) TableName() string { return "legs" }

type outboxModel struct {
	ID          string     `gorm:"column:id;primaryKey"`
	EventType   string     `gorm:"column:event_type"`
	Payload     []byte     `gorm:"column:payload"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	PublishedAt *time.Time `gorm:"column:published_at"`
}

func (outboxModel) TableName() string { return "outbox" }

type closingSnapshotModel struct {
	ID            string    `gorm:"column:id;primaryKey"`
	ParticipantID string    `gorm:"column:participant_id"`
	BalanceMinor  int64     `gorm:"column:balance_minor"`
	Currency      string    `gorm:"column:currency"`
	AsOf          time.Time `gorm:"column:as_of"`
	CreatedAt     time.Time `gorm:"column:created_at"`
}

func (closingSnapshotModel) TableName() string { return "daily_closing_snapshots" }
