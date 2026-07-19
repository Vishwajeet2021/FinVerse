package postgres

import "time"

type customerModel struct {
	ID           string    `gorm:"column:id;primaryKey"`
	UserID       string    `gorm:"column:user_id"`
	FullName     string    `gorm:"column:full_name"`
	DOB          time.Time `gorm:"column:dob"`
	PANEncrypted []byte    `gorm:"column:pan_encrypted"`
	PANMasked    string    `gorm:"column:pan_masked"`
	KYCTier      int       `gorm:"column:kyc_tier"`
	KYCStatus    string    `gorm:"column:kyc_status"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

func (customerModel) TableName() string { return "customers" }

type kycDocumentModel struct {
	ID         string    `gorm:"column:id;primaryKey"`
	CustomerID string    `gorm:"column:customer_id"`
	DocType    string    `gorm:"column:doc_type"`
	StorageKey string    `gorm:"column:storage_key"`
	UploadedAt time.Time `gorm:"column:uploaded_at"`
}

func (kycDocumentModel) TableName() string { return "kyc_documents_meta" }

type beneficiaryModel struct {
	ID         string    `gorm:"column:id;primaryKey"`
	CustomerID string    `gorm:"column:customer_id"`
	Name       string    `gorm:"column:name"`
	AccountRef string    `gorm:"column:account_ref"`
	Verified   bool      `gorm:"column:verified"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (beneficiaryModel) TableName() string { return "beneficiaries" }
