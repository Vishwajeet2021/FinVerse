// Package http implements Customer Service's REST contract per
// docs/09-api-standards.md. Handlers validate input, call internal/service,
// and map results to the standard envelope — no business logic lives here,
// per docs/12-coding-standards.md §1.1.
package http

import (
	"encoding/base64"
	"time"

	"github.com/finverse/customer-service/internal/domain"
	"github.com/finverse/customer-service/internal/service"
	"github.com/finverse/shared/apiresponse"
	"github.com/finverse/shared/jwtauth"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	customers *service.CustomerService
}

func NewHandler(customers *service.CustomerService) *Handler {
	return &Handler{customers: customers}
}

// resolveOwnCustomer implements the structural ownership check required by
// docs/10-authentication-authorization.md §3.3: every `/me` route resolves
// "which customer" from the authenticated caller's own JWT subject
// (the User ID), never from a client-supplied customer ID — so there is no
// runtime check to forget, the route simply has no way to name someone
// else's resource.
func (h *Handler) resolveOwnCustomer(c *gin.Context) (*domain.Customer, bool) {
	claims := jwtauth.ClaimsFromContext(c)
	customer, err := h.customers.GetProfileByUserID(c.Request.Context(), claims.Subject)
	if err != nil {
		writeDomainError(c, err)
		return nil, false
	}
	return customer, true
}

// GetKYCTierByUserID is an internal, service-to-service endpoint (no JWT
// middleware — internal network trust, the same posture as Transaction
// Service's plaintext gRPC per docs/11-security-standards.md §7) that lets
// another service resolve a user's live KYC tier without requiring that
// user's own JWT. It exists specifically because Auth Service's JWT
// kyc_tier claim is a documented Phase 1/2 stand-in that's always 0 (see
// services/auth-service/internal/service/login.go's comment) — Account
// Service's Tier-1+ gate for FR-ACC-1 calls this instead of trusting that
// claim. Returns 404 if the user has no customer profile yet.
func (h *Handler) GetKYCTierByUserID(c *gin.Context) {
	customer, err := h.customers.GetProfileByUserID(c.Request.Context(), c.Param("userId"))
	if err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, gin.H{"kycTier": customer.KYCTier, "kycStatus": customer.KYCStatus})
}

// CreateProfile implements FR-CUST-1.
func (h *Handler) CreateProfile(c *gin.Context) {
	var req createProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "Please check your input and try again.")
		return
	}

	dob, err := time.Parse(dateLayout, req.DOB)
	if err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "dob must be a date in YYYY-MM-DD format.")
		return
	}

	claims := jwtauth.ClaimsFromContext(c)
	customer, err := h.customers.CreateProfile(c.Request.Context(), claims.Subject, req.FullName, dob, req.PAN)
	if err != nil {
		writeDomainError(c, err)
		return
	}

	apiresponse.Created(c, toCustomerResponse(customer))
}

// GetMe implements the read side of FR-CUST-5, resolved via claims.Subject
// (see resolveOwnCustomer) rather than any customer ID in the URL.
func (h *Handler) GetMe(c *gin.Context) {
	customer, ok := h.resolveOwnCustomer(c)
	if !ok {
		return
	}
	apiresponse.OK(c, toCustomerResponse(customer))
}

// UpdateMe implements FR-CUST-5: only non-KYC-affecting fields.
func (h *Handler) UpdateMe(c *gin.Context) {
	customer, ok := h.resolveOwnCustomer(c)
	if !ok {
		return
	}

	var req updateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "Please check your input and try again.")
		return
	}

	updated, err := h.customers.UpdateProfile(c.Request.Context(), customer.ID, req.FullName)
	if err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, toCustomerResponse(updated))
}

// SubmitKYC implements FR-CUST-3's PENDING -> UNDER_REVIEW transition (and,
// in dev, the auto-approve convenience — see internal/service/kyc.go).
func (h *Handler) SubmitKYC(c *gin.Context) {
	customer, ok := h.resolveOwnCustomer(c)
	if !ok {
		return
	}

	updated, err := h.customers.SubmitForReview(c.Request.Context(), customer.ID)
	if err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, toCustomerResponse(updated))
}

// ReviewKYC implements FR-CUST-3's UNDER_REVIEW -> APPROVED|REJECTED
// transition. This is the one endpoint in this service where the caller is
// deliberately not the resource owner (docs/10-authentication-authorization.md §3.3
// calls this out as the exception, not a missed check): it is gated by the
// `customers:kyc:review` scope (see routes.go), which today only an
// admin/super_admin-rolled JWT carries (internal/service/scopes.go in
// Auth Service). Until Admin Service exists (Phase 6,
// docs/07-microservice-specifications.md §13), the only way to obtain such a
// JWT is a role seeded directly in the Auth DB for testing — there is no
// self-service path to become admin, which is correct.
func (h *Handler) ReviewKYC(c *gin.Context) {
	customerID := c.Param("id")

	var req kycReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "Please check your input and try again.")
		return
	}

	updated, err := h.customers.ReviewKYC(c.Request.Context(), customerID, req.Approve, domain.KYCTier(req.Tier))
	if err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.OK(c, toCustomerResponse(updated))
}

// AddDocument implements FR-CUST-2. The document is JSON+base64
// (see dto.go's addDocumentRequest for why), decoded here and handed to the
// service layer as raw bytes — the service layer never knows or cares how
// the transport got the bytes.
func (h *Handler) AddDocument(c *gin.Context) {
	customer, ok := h.resolveOwnCustomer(c)
	if !ok {
		return
	}

	var req addDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "Please check your input and try again.")
		return
	}

	data, err := base64.StdEncoding.DecodeString(req.ContentBase64)
	if err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "contentBase64 must be valid base64.")
		return
	}

	doc, err := h.customers.AddDocument(c.Request.Context(), customer.ID, req.DocType, req.Filename, data)
	if err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.Created(c, toDocumentResponse(doc))
}

// AddBeneficiary implements FR-CUST-4.
func (h *Handler) AddBeneficiary(c *gin.Context) {
	customer, ok := h.resolveOwnCustomer(c)
	if !ok {
		return
	}

	var req addBeneficiaryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "Please check your input and try again.")
		return
	}

	beneficiary, err := h.customers.AddBeneficiary(c.Request.Context(), customer.ID, req.Name, req.AccountRef)
	if err != nil {
		writeDomainError(c, err)
		return
	}
	apiresponse.Created(c, toBeneficiaryResponse(beneficiary))
}

// ListBeneficiaries implements the read side of FR-CUST-4.
func (h *Handler) ListBeneficiaries(c *gin.Context) {
	customer, ok := h.resolveOwnCustomer(c)
	if !ok {
		return
	}

	beneficiaries, err := h.customers.ListBeneficiaries(c.Request.Context(), customer.ID)
	if err != nil {
		writeDomainError(c, err)
		return
	}

	responses := make([]beneficiaryResponse, 0, len(beneficiaries))
	for _, b := range beneficiaries {
		responses = append(responses, toBeneficiaryResponse(b))
	}
	apiresponse.List(c, responses, 1, len(responses), len(responses))
}
