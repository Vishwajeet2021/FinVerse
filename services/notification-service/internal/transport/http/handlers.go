package http

import (
	"net/http"
	"strconv"

	"github.com/finverse/notification-service/internal/sender"
	"github.com/finverse/notification-service/internal/service"
	"github.com/finverse/shared/apiresponse"
	"github.com/finverse/shared/eventbus"
	"github.com/finverse/shared/jwtauth"
	"github.com/gin-gonic/gin"
)

// Bus is what the admin API needs from the event consumers. Keyed by stream
// (event type) so one endpoint can inspect every DLQ this service owns.
type Bus struct {
	Consumers map[string]*eventbus.Consumer
}

type Handler struct {
	svc    *service.Service
	bus    Bus
	sender *sender.LogSender
}

func NewHandler(svc *service.Service, bus Bus, s *sender.LogSender) *Handler {
	return &Handler{svc: svc, bus: bus, sender: s}
}

func limitParam(c *gin.Context) int {
	n, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || n < 1 {
		return 50
	}
	if n > 200 {
		return 200
	}
	return n
}

// ListMine is the customer inbox (GET /notifications). Ownership is the
// customer_id claim, never a query parameter.
func (h *Handler) ListMine(c *gin.Context) {
	claims := jwtauth.ClaimsFromContext(c)
	if claims == nil || claims.CustomerID == "" {
		apiresponse.Forbidden(c, "FORBIDDEN", "no customer profile on this token")
		return
	}
	items, err := h.svc.ListForRecipient(c.Request.Context(), "customer:"+claims.CustomerID, limitParam(c))
	if err != nil {
		apiresponse.Error(c, http.StatusInternalServerError, "INTERNAL", "could not list notifications", nil)
		return
	}
	apiresponse.OK(c, items)
}

func (h *Handler) ListAll(c *gin.Context) {
	items, err := h.svc.ListAll(c.Request.Context(), limitParam(c))
	if err != nil {
		apiresponse.Error(c, http.StatusInternalServerError, "INTERNAL", "could not list notifications", nil)
		return
	}
	apiresponse.OK(c, items)
}

// DLQList returns dead-lettered messages across every stream this service consumes.
func (h *Handler) DLQList(c *gin.Context) {
	out := []eventbus.DLQEntry{}
	for _, cons := range h.bus.Consumers {
		entries, err := cons.DLQ().List(c.Request.Context(), int64(limitParam(c)))
		if err != nil {
			apiresponse.Error(c, http.StatusInternalServerError, "INTERNAL", "could not read DLQ", nil)
			return
		}
		out = append(out, entries...)
	}
	apiresponse.OK(c, out)
}

func (h *Handler) consumerFor(c *gin.Context) (*eventbus.Consumer, bool) {
	cons, ok := h.bus.Consumers[c.Param("stream")]
	if !ok {
		apiresponse.NotFound(c, "NOT_FOUND", "unknown stream")
	}
	return cons, ok
}

// DLQReplay puts one dead letter back on its stream for this service only.
func (h *Handler) DLQReplay(c *gin.Context) {
	cons, ok := h.consumerFor(c)
	if !ok {
		return
	}
	if err := cons.DLQ().Replay(c.Request.Context(), c.Param("id")); err != nil {
		apiresponse.NotFound(c, "NOT_FOUND", err.Error())
		return
	}
	apiresponse.OK(c, gin.H{"replayed": c.Param("id")})
}

func (h *Handler) DLQDiscard(c *gin.Context) {
	cons, ok := h.consumerFor(c)
	if !ok {
		return
	}
	if err := cons.DLQ().Discard(c.Request.Context(), c.Param("id")); err != nil {
		apiresponse.Error(c, http.StatusInternalServerError, "INTERNAL", "could not discard", nil)
		return
	}
	apiresponse.NoContent(c)
}

// Stats reports per-stream consumer counters and DLQ depth (NFR-OBS-2).
func (h *Handler) Stats(c *gin.Context) {
	type row struct {
		eventbus.Stats
		DLQDepth int64 `json:"dlqDepth"`
	}
	out := map[string]row{}
	for stream, cons := range h.bus.Consumers {
		depth, _ := cons.DLQ().Len(c.Request.Context())
		out[stream] = row{Stats: cons.Stats(), DLQDepth: depth}
	}
	apiresponse.OK(c, gin.H{"streams": out, "providerFailing": h.sender.Failing(), "delivered": h.sender.Sent()})
}

// SetProviderOutage flips the simulated email-provider outage. It exists so
// the retry → DLQ → replay path is demonstrable on demand; the route is not
// registered in production (see NewRouter).
func (h *Handler) SetProviderOutage(c *gin.Context) {
	var body struct {
		Failing bool `json:"failing"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiresponse.BadRequest(c, "VALIDATION_ERROR", "body must be {\"failing\": bool}")
		return
	}
	h.sender.SetFailing(body.Failing)
	apiresponse.OK(c, gin.H{"providerFailing": body.Failing})
}
