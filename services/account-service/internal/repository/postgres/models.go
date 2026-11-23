package postgres

import "time"

type accountModel struct {
	ID                  string    `gorm:"column:id;primaryKey"`
	CustomerID          string    `gorm:"column:customer_id"`
	AccountNumber       string    `gorm:"column:account_number"`
	ProductType         string    `gorm:"column:product_type"`
	Status              string    `gorm:"column:status"`
	OverdraftLimitMinor int64     `gorm:"column:overdraft_limit_minor"`
	CreatedAt           time.Time `gorm:"column:created_at"`
}

func (accountModel) TableName() string { return "accounts" }

type accountHolderModel struct {
	AccountID  string `gorm:"column:account_id;primaryKey"`
	CustomerID string `gorm:"column:customer_id;primaryKey"`
	Role       string `gorm:"column:role"`
}

func (accountHolderModel) TableName() string { return "account_holders" }

type balanceProjectionModel struct {
	AccountID             string    `gorm:"column:account_id;primaryKey"`
	LedgerBalanceMinor    int64     `gorm:"column:ledger_balance_minor"`
	AvailableBalanceMinor int64     `gorm:"column:available_balance_minor"`
	LastTransactionID     *string   `gorm:"column:last_transaction_id"`
	UpdatedAt             time.Time `gorm:"column:updated_at"`
}

func (balanceProjectionModel) TableName() string { return "balance_projection" }

type outboxModel struct {
	ID          string     `gorm:"column:id;primaryKey"`
	EventType   string     `gorm:"column:event_type"`
	Payload     []byte     `gorm:"column:payload"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	PublishedAt *time.Time `gorm:"column:published_at"`
	TraceParent *string    `gorm:"column:traceparent"`
}

func (outboxModel) TableName() string { return "outbox" }

type processedEventModel struct {
	EventID     string    `gorm:"column:event_id;primaryKey"`
	ProcessedAt time.Time `gorm:"column:processed_at"`
}

func (processedEventModel) TableName() string { return "processed_events" }
