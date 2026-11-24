// Package http is Reporting Service's REST surface (docs/23).
package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/finverse/reporting-service/internal/service"
	"github.com/finverse/shared/apiresponse"
	"github.com/finverse/shared/jwtauth"
	"github.com/gin-gonic/gin"
)

type Handler struct{ svc *service.Service }

func NewHandler(svc *service.Service) *Handler { return &Handler{svc: svc} }

func caller(c *gin.Context) string { return jwtauth.ClaimsFromContext(c).Subject }

func bearer(c *gin.Context) string {
	return strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidInput):
		apiresponse.BadRequest(c, "INVALID_REQUEST", strings.TrimPrefix(err.Error(), "invalid input: "))
	case errors.Is(err, service.ErrNotFound):
		apiresponse.NotFound(c, "REPORT_NOT_FOUND", "No such report or account.")
	case errors.Is(err, service.ErrNotReady):
		apiresponse.Conflict(c, "REPORT_NOT_READY", "This report is not ready to download.")
	case errors.Is(err, service.ErrTooManyActive):
		apiresponse.TooManyRequests(c, "TOO_MANY_REPORTS", "You already have several reports in progress. Wait for one to finish.")
	case errors.Is(err, service.ErrUpstream):
		apiresponse.Error(c, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE", "A service this report depends on is unavailable. Please try again.", nil)
	default:
		apiresponse.Internal(c, "INTERNAL_ERROR", "Something went wrong on our end. Please try again.")
	}
}

func (h *Handler) RequestStatement(c *gin.Context) {
	var b struct {
		AccountID string `json:"accountId" binding:"required"`
		From      string `json:"from" binding:"required"`
		To        string `json:"to" binding:"required"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "accountId, from and to are required.")
		return
	}
	r, err := h.svc.RequestStatement(c.Request.Context(), caller(c), bearer(c), b.AccountID, b.From, b.To)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Location", "/api/v1/reports/"+r.ID)
	apiresponse.Accepted(c, r)
}

func (h *Handler) Get(c *gin.Context) {
	r, err := h.svc.Get(c.Request.Context(), caller(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.OK(c, r)
}

func (h *Handler) List(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	list, err := h.svc.List(c.Request.Context(), caller(c), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.OK(c, list)
}

func (h *Handler) RequestFraudSummary(c *gin.Context) {
	r, err := h.svc.RequestFraudSummary(c.Request.Context(), caller(c), bearer(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Location", "/api/v1/reports/"+r.ID)
	apiresponse.Accepted(c, r)
}

// Download streams the file. Headers are fixed here, none of them derived from stored or client
// input except the server-generated filename.
func (h *Handler) Download(c *gin.Context) {
	data, name, err := h.svc.Download(c.Request.Context(), caller(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}
