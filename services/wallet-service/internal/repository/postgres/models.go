package postgres

import "time"

type walletModel struct {
	ID               string    `gorm:"column:id;primaryKey"`
	CustomerID       string    `gorm:"column:customer_id"`
	FundingAccountID string    `gorm:"column:funding_account_id"`
	Status           string    `gorm:"column:status"`
	CreatedAt        time.Time `gorm:"column:created_at"`
}

func (walletModel) TableName() string { return "wallets" }

type balanceProjectionModel struct {
	WalletID          string    `gorm:"column:wallet_id;primaryKey"`
	BalanceMinor      int64     `gorm:"column:balance_minor"`
	LastTransactionID *string   `gorm:"column:last_transaction_id"`
	UpdatedAt         time.Time `gorm:"column:updated_at"`
}

func (balanceProjectionModel) TableName() string { return "balance_projection" }

type outboxModel struct {
	ID          string     `gorm:"column:id;primaryKey"`
	EventType   string     `gorm:"column:event_type"`
	Payload     []byte     `gorm:"column:payload"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	PublishedAt *time.Time `gorm:"column:published_at"`
}

func (outboxModel) TableName() string { return "outbox" }

type processedEventModel struct {
	EventID     string    `gorm:"column:event_id;primaryKey"`
	ProcessedAt time.Time `gorm:"column:processed_at"`
}

func (processedEventModel) TableName() string { return "processed_events" }
