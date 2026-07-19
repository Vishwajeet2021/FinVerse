// Package apiresponse implements the request/response and error envelope
// defined in docs/09-api-standards.md §2.3-2.4, shared by every service so
// the 15 services feel like one API.
package apiresponse

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Meta accompanies every successful response.
type Meta struct {
	RequestID string `json:"requestId"`
	TraceID   string `json:"traceId,omitempty"`
}

type successEnvelope struct {
	Data any  `json:"data"`
	Meta Meta `json:"meta"`
}

// ListMeta accompanies paginated list responses (docs/09-api-standards.md §2.7).
type ListMeta struct {
	Meta
	Page       int `json:"page,omitempty"`
	PageSize   int `json:"pageSize,omitempty"`
	TotalCount int `json:"totalCount,omitempty"`
}

type listEnvelope struct {
	Data any      `json:"data"`
	Meta ListMeta `json:"meta"`
}

// ErrorDetail is the stable, machine-readable error shape (docs/09-api-standards.md §2.4).
type ErrorDetail struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	RequestID string         `json:"requestId"`
	TraceID   string         `json:"traceId,omitempty"`
}

type errorEnvelope struct {
	Error ErrorDetail `json:"error"`
}

func requestID(c *gin.Context) string {
	if v, ok := c.Get("request_id"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// OK writes a 200 success envelope.
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, successEnvelope{Data: data, Meta: Meta{RequestID: requestID(c)}})
}

// Created writes a 201 success envelope.
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, successEnvelope{Data: data, Meta: Meta{RequestID: requestID(c)}})
}

// NoContent writes a 204 with no body.
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// List writes a paginated success envelope.
func List(c *gin.Context, data any, page, pageSize, totalCount int) {
	c.JSON(http.StatusOK, listEnvelope{
		Data: data,
		Meta: ListMeta{Meta: Meta{RequestID: requestID(c)}, Page: page, PageSize: pageSize, TotalCount: totalCount},
	})
}

// Error writes the standard error envelope at the given HTTP status.
// code must be a stable, SCREAMING_SNAKE_CASE identifier per the error
// registry convention in docs/09-api-standards.md §2.4.
func Error(c *gin.Context, status int, code, message string, details map[string]any) {
	c.AbortWithStatusJSON(status, errorEnvelope{Error: ErrorDetail{
		Code:      code,
		Message:   message,
		Details:   details,
		RequestID: requestID(c),
	}})
}

// Common error helpers for the status mapping in docs/09-api-standards.md §2.5.

func BadRequest(c *gin.Context, code, message string) {
	Error(c, http.StatusBadRequest, code, message, nil)
}

func Unauthorized(c *gin.Context, code, message string) {
	Error(c, http.StatusUnauthorized, code, message, nil)
}

func Forbidden(c *gin.Context, code, message string) {
	Error(c, http.StatusForbidden, code, message, nil)
}

func NotFound(c *gin.Context, code, message string) {
	Error(c, http.StatusNotFound, code, message, nil)
}

func Conflict(c *gin.Context, code, message string) {
	Error(c, http.StatusConflict, code, message, nil)
}

func UnprocessableEntity(c *gin.Context, code, message string) {
	Error(c, http.StatusUnprocessableEntity, code, message, nil)
}

func TooManyRequests(c *gin.Context, code, message string) {
	Error(c, http.StatusTooManyRequests, code, message, nil)
}

func Internal(c *gin.Context, code, message string) {
	Error(c, http.StatusInternalServerError, code, message, nil)
}
