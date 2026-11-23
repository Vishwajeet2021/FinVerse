package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/finverse/fraud-service/internal/service"
	"github.com/finverse/shared/apiresponse"
	"github.com/finverse/shared/jwtauth"
	"github.com/gin-gonic/gin"
)

type Handler struct{ svc *service.Service }

func NewHandler(svc *service.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) ListCases(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	cases, err := h.svc.ListCases(c.Request.Context(), c.Query("status"), limit)
	if err != nil {
		apiresponse.Error(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not list cases.", nil)
		return
	}
	apiresponse.OK(c, cases)
}

func (h *Handler) ResolveCase(c *gin.Context) {
	var body struct {
		Outcome string `json:"outcome" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiresponse.BadRequest(c, "INVALID_REQUEST", "outcome is required (CONFIRMED or DISMISSED).")
		return
	}
	actor := "unknown"
	if claims := jwtauth.ClaimsFromContext(c); claims != nil {
		actor = claims.Subject
	}
	res, err := h.svc.ResolveCase(c.Request.Context(), c.Param("id"), body.Outcome, actor)
	switch {
	case err == nil:
		apiresponse.OK(c, res)
	case errors.Is(err, service.ErrCaseNotFound):
		apiresponse.NotFound(c, "CASE_NOT_FOUND", "No such case.")
	case errors.Is(err, service.ErrCaseNotOpen):
		apiresponse.Conflict(c, "CASE_ALREADY_RESOLVED", "This case is already resolved.")
	case errors.Is(err, service.ErrInvalidOutcome):
		apiresponse.BadRequest(c, "INVALID_OUTCOME", err.Error())
	default:
		apiresponse.Error(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not resolve the case.", nil)
	}
}
