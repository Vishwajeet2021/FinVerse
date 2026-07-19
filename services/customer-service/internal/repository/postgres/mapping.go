package postgres

import "github.com/finverse/customer-service/internal/domain"

func toCustomerModel(c *domain.Customer) *customerModel {
	return &customerModel{
		ID:           c.ID,
		UserID:       c.UserID,
		FullName:     c.FullName,
		DOB:          c.DOB,
		PANEncrypted: c.PANEncrypted,
		PANMasked:    c.PANMasked,
		KYCTier:      int(c.KYCTier),
		KYCStatus:    string(c.KYCStatus),
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
	}
}

func fromCustomerModel(m *customerModel) *domain.Customer {
	return &domain.Customer{
		ID:           m.ID,
		UserID:       m.UserID,
		FullName:     m.FullName,
		DOB:          m.DOB,
		PANEncrypted: m.PANEncrypted,
		PANMasked:    m.PANMasked,
		KYCTier:      domain.KYCTier(m.KYCTier),
		KYCStatus:    domain.KYCStatus(m.KYCStatus),
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}

func toDocumentModel(d *domain.KYCDocument) *kycDocumentModel {
	return &kycDocumentModel{
		ID:         d.ID,
		CustomerID: d.CustomerID,
		DocType:    d.DocType,
		StorageKey: d.StorageKey,
		UploadedAt: d.UploadedAt,
	}
}

func fromDocumentModel(m *kycDocumentModel) *domain.KYCDocument {
	return &domain.KYCDocument{
		ID:         m.ID,
		CustomerID: m.CustomerID,
		DocType:    m.DocType,
		StorageKey: m.StorageKey,
		UploadedAt: m.UploadedAt,
	}
}

func toBeneficiaryModel(b *domain.Beneficiary) *beneficiaryModel {
	return &beneficiaryModel{
		ID:         b.ID,
		CustomerID: b.CustomerID,
		Name:       b.Name,
		AccountRef: b.AccountRef,
		Verified:   b.Verified,
		CreatedAt:  b.CreatedAt,
	}
}

func fromBeneficiaryModel(m *beneficiaryModel) *domain.Beneficiary {
	return &domain.Beneficiary{
		ID:         m.ID,
		CustomerID: m.CustomerID,
		Name:       m.Name,
		AccountRef: m.AccountRef,
		Verified:   m.Verified,
		CreatedAt:  m.CreatedAt,
	}
}
