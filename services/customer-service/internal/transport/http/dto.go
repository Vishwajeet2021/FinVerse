package http

import (
	"time"

	"github.com/finverse/customer-service/internal/domain"
)

const dateLayout = "2006-01-02"

type createProfileRequest struct {
	FullName string `json:"fullName" binding:"required,max=480"`
	DOB      string `json:"dob" binding:"required,max=10"`
	PAN      string `json:"pan" binding:"required,max=16"`
}

type updateProfileRequest struct {
	FullName string `json:"fullName" binding:"required,max=480"`
}

// kycReviewRequest is the admin-only decision payload for
// POST /api/v1/customers/{id}/kyc/review (FR-CUST-3). Tier is only
// meaningful when Approve is true; validated in the service layer
// (internal/service/kyc.go), not here, since "which tiers are valid" is a
// business rule, not an input-shape rule.
type kycReviewRequest struct {
	Approve bool `json:"approve"`
	Tier    int  `json:"tier"`
}

// addDocumentRequest carries the document as base64-encoded JSON rather than
// multipart/form-data — this service's Phase 1 scope has no other
// binary-upload endpoint to justify the extra multipart parsing path, and a
// JSON body keeps the request shape consistent with every other endpoint's
// envelope (docs/09-api-standards.md §2.3). ContentBase64 size is bounded by
// Gin's default request body limit; a dedicated size/magic-byte check
// belongs to the same hardening pass that adds real S3 storage
// (docs/11-security-standards.md §5), tracked as a gap for that later phase.
type addDocumentRequest struct {
	DocType       string `json:"docType" binding:"required,max=32"`
	Filename      string `json:"filename" binding:"required,max=512"`
	ContentBase64 string `json:"contentBase64" binding:"required,max=7500000"` // 5 MiB of data, base64 encoded
}

type addBeneficiaryRequest struct {
	Name       string `json:"name" binding:"required,max=480"`
	AccountRef string `json:"accountRef" binding:"required,max=64"`
}

type customerResponse struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	FullName  string `json:"fullName"`
	DOB       string `json:"dob"`
	PANMasked string `json:"panMasked"`
	KYCTier   int    `json:"kycTier"`
	KYCStatus string `json:"kycStatus"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func toCustomerResponse(c *domain.Customer) customerResponse {
	return customerResponse{
		ID:        c.ID,
		UserID:    c.UserID,
		FullName:  c.FullName,
		DOB:       c.DOB.Format(dateLayout),
		PANMasked: c.PANMasked,
		KYCTier:   int(c.KYCTier),
		KYCStatus: string(c.KYCStatus),
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
		UpdatedAt: c.UpdatedAt.Format(time.RFC3339),
	}
}

type documentResponse struct {
	ID         string `json:"id"`
	DocType    string `json:"docType"`
	StorageKey string `json:"storageKey"`
	UploadedAt string `json:"uploadedAt"`
}

func toDocumentResponse(d *domain.KYCDocument) documentResponse {
	return documentResponse{
		ID:         d.ID,
		DocType:    d.DocType,
		StorageKey: d.StorageKey,
		UploadedAt: d.UploadedAt.Format(time.RFC3339),
	}
}

type beneficiaryResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AccountRef string `json:"accountRef"`
	Verified   bool   `json:"verified"`
	CreatedAt  string `json:"createdAt"`
}

func toBeneficiaryResponse(b *domain.Beneficiary) beneficiaryResponse {
	return beneficiaryResponse{
		ID:         b.ID,
		Name:       b.Name,
		AccountRef: b.AccountRef,
		Verified:   b.Verified,
		CreatedAt:  b.CreatedAt.Format(time.RFC3339),
	}
}
