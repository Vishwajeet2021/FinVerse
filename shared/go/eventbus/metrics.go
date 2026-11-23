package eventbus

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	messagesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "finverse_eventbus_messages_total",
		Help: "Messages handled by a consumer group, by outcome (processed, retried, dead_lettered, skipped).",
	}, []string{"stream", "group", "outcome"})

	dlqDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "finverse_eventbus_dlq_depth",
		Help: "Messages currently in a consumer group's dead-letter stream.",
	}, []string{"stream", "group"})
)

func (c *Consumer) count(outcome string) {
	messagesTotal.WithLabelValues(c.stream, c.group, outcome).Inc()
}

// reportDLQDepth refreshes the depth gauge until ctx ends. Polled rather than
// event-driven so replays and discards (done by operators, possibly from
// another replica) are reflected too.
func (c *Consumer) reportDLQDepth(ctx context.Context) {
	g := dlqDepth.WithLabelValues(c.stream, c.group)
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		if n, err := c.DLQ().Len(qctx); err == nil {
			g.Set(float64(n))
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
