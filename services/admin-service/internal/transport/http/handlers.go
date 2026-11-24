// Package http is Admin Service's REST surface (docs/23).
package http

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/finverse/admin-service/internal/service"
	"github.com/finverse/shared/apiresponse"
	"github.com/finverse/shared/jwtauth"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
)

type Handler struct {
	svc     *service.Service
	metrics service.Metrics
}

func NewHandler(svc *service.Service, m service.Metrics) *Handler {
	return &Handler{svc: svc, metrics: m}
}

func actorID(c *gin.Context) string { return jwtauth.ClaimsFromContext(c).Subject }

// bearer returns the caller's access token so it can be forwarded (docs/23 D-A3). The JWT
// middleware has already validated it; absence here would be a wiring bug.
func bearer(c *gin.Context) string {
	return strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
}

func traceID(c *gin.Context) string {
	if sc := trace.SpanContextFromContext(c.Request.Context()); sc.IsValid() {
		return sc.TraceID().String()
	}
	return ""
}

// writeError maps service errors to the API error envelope. Downstream client errors (404,
// 409, 422...) are passed through with their code so the console can show what happened;
// anything else downstream becomes a 502 without leaking internals.
func writeError(c *gin.Context, err error) {
	var de *service.DownstreamError
	switch {
	case errors.Is(err, service.ErrInvalidReason):
		apiresponse.BadRequest(c, "INVALID_REASON", err.Error())
	case errors.Is(err, service.ErrInvalidTarget):
		apiresponse.BadRequest(c, "INVALID_TARGET", "The target id is not valid.")
	case errors.Is(err, service.ErrInvalidInput):
		apiresponse.BadRequest(c, "INVALID_REQUEST", err.Error())
	case errors.Is(err, service.ErrAuditUnavailable):
		apiresponse.Error(c, http.StatusServiceUnavailable, "AUDIT_UNAVAILABLE", "The action was not performed because it could not be recorded. Try again shortly.", nil)
	case errors.As(err, &de) && !de.Server():
		apiresponse.Error(c, de.Status, "DOWNSTREAM_"+de.Code, "The target service refused the action.", map[string]any{"downstreamCode": de.Code})
	default:
		apiresponse.Error(c, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE", "The target service could not be reached. The attempt has been recorded.", nil)
	}
}

type reasonBody struct {
	Reason string `json:"reason" binding:"required"`
}

func (h *Handler) respond(c *gin.Context, a *service.Action, err error) {
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.OK(c, a)
}

func (h *Handler) Dashboard(c *gin.Context) {
	apiresponse.OK(c, h.svc.Dashboard(c.Request.Context(), bearer(c), h.metrics))
}

func (h *Handler) ListActions(c *gin.Context) {
	var before *time.Time
	if v := c.Query("before"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			apiresponse.BadRequest(c, "INVALID_REQUEST", "before must be an RFC3339 timestamp.")
			return
		}
		before = &t
	}
	limit := 50
	if v := c.Query("limit"); v != "" {
		n := 0
		for _, ch := range v {
			if ch < '0' || ch > '9' || n > 1000 {
				apiresponse.BadRequest(c, "INVALID_REQUEST", "limit must be a number.")
				return
			}
			n = n*10 + int(ch-'0')
		}
		limit = n
	}
	list, err := h.svc.ListActions(c.Request.Context(), limit, before)
	if err != nil {
		apiresponse.Internal(c, "INTERNAL_ERROR", "Could not read admin actions.")
		return
	}
	apiresponse.OK(c, list)
}

func (h *Handler) Freeze(c *gin.Context) {
	var b reasonBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "A reason is required.")
		return
	}
	a, err := h.svc.FreezeAccount(c.Request.Context(), actorID(c), c.Param("id"), b.Reason, traceID(c))
	h.respond(c, a, err)
}

func (h *Handler) Unfreeze(c *gin.Context) {
	var b reasonBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "A reason is required.")
		return
	}
	a, err := h.svc.UnfreezeAccount(c.Request.Context(), actorID(c), c.Param("id"), b.Reason, traceID(c))
	h.respond(c, a, err)
}

func (h *Handler) ResolveFraudCase(c *gin.Context) {
	var b struct {
		Outcome string `json:"outcome" binding:"required"`
		Reason  string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "outcome and reason are required.")
		return
	}
	a, err := h.svc.ResolveFraudCase(c.Request.Context(), actorID(c), bearer(c), c.Param("id"), b.Outcome, b.Reason, traceID(c))
	h.respond(c, a, err)
}

func (h *Handler) ReviewKYC(c *gin.Context) {
	var b struct {
		Approve *bool  `json:"approve" binding:"required"`
		Tier    int    `json:"tier"`
		Reason  string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "approve and reason are required.")
		return
	}
	a, err := h.svc.ReviewKYC(c.Request.Context(), actorID(c), bearer(c), c.Param("id"), *b.Approve, b.Tier, b.Reason, traceID(c))
	h.respond(c, a, err)
}
