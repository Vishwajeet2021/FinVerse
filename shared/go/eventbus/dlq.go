package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// DLQName is the dead-letter stream for one consuming service on one event
// stream. It is per-(stream, group), not per-stream: each service owns its
// own DLQ so one service's poison messages never mask or get replayed into
// another's (docs/05-system-architecture.md §7 bulkhead).
func DLQName(stream, group string) string { return stream + ":dlq:" + group }

// DLQEntry is one dead-lettered message with enough context to diagnose it
// and, via Replay, to put it back on the stream.
type DLQEntry struct {
	ID             string    `json:"id"` // ID within the DLQ stream; pass to Replay/Discard
	Stream         string    `json:"stream"`
	Group          string    `json:"group"`
	OriginalID     string    `json:"originalId"`
	Reason         string    `json:"reason"`
	Attempts       int64     `json:"attempts"`
	DeadLetteredAt time.Time `json:"deadLetteredAt"`
	Envelope       *Envelope `json:"envelope,omitempty"`
}

// DLQ is the operator-facing view of a dead-letter stream: inspect, replay,
// discard. It is what turns "failures land somewhere visible"
// (docs/05-system-architecture.md §7) into something actionable.
type DLQ struct {
	client *redis.Client
	stream string
	group  string
}

func NewDLQ(client *redis.Client, stream, group string) *DLQ {
	return &DLQ{client: client, stream: stream, group: group}
}

func (d *DLQ) name() string { return DLQName(d.stream, d.group) }

func (d *DLQ) Len(ctx context.Context) (int64, error) {
	return d.client.XLen(ctx, d.name()).Result()
}

// List returns up to count entries, oldest first.
func (d *DLQ) List(ctx context.Context, count int64) ([]DLQEntry, error) {
	msgs, err := d.client.XRangeN(ctx, d.name(), "-", "+", count).Result()
	if err != nil {
		return nil, fmt.Errorf("reading DLQ: %w", err)
	}
	out := make([]DLQEntry, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, d.toEntry(m))
	}
	return out, nil
}

func (d *DLQ) toEntry(m redis.XMessage) DLQEntry {
	e := DLQEntry{ID: m.ID, Stream: d.stream, Group: d.group}
	if v, ok := m.Values["original_id"].(string); ok {
		e.OriginalID = v
	}
	if v, ok := m.Values["reason"].(string); ok {
		e.Reason = v
	}
	if v, ok := m.Values["attempts"].(string); ok {
		e.Attempts, _ = strconv.ParseInt(v, 10, 64)
	}
	if v, ok := m.Values["dead_lettered_at"].(string); ok {
		e.DeadLetteredAt, _ = time.Parse(time.RFC3339, v)
	}
	if v, ok := m.Values["envelope"].(string); ok {
		var env Envelope
		if json.Unmarshal([]byte(v), &env) == nil {
			e.Envelope = &env
		}
	}
	return e
}

// Replay re-publishes one dead-lettered message to its original stream,
// addressed to this DLQ's group only (target_group) so the other consuming
// services — which already handled it — don't see a duplicate. The DLQ entry
// is removed only after the re-publish succeeds.
func (d *DLQ) Replay(ctx context.Context, dlqID string) error {
	msgs, err := d.client.XRangeN(ctx, d.name(), dlqID, dlqID, 1).Result()
	if err != nil {
		return fmt.Errorf("reading DLQ entry: %w", err)
	}
	if len(msgs) == 0 {
		return fmt.Errorf("DLQ entry %s not found", dlqID)
	}
	raw, ok := msgs[0].Values["envelope"].(string)
	if !ok {
		return fmt.Errorf("DLQ entry %s has no stored envelope and cannot be replayed", dlqID)
	}
	if err := d.client.XAdd(ctx, &redis.XAddArgs{
		Stream: d.stream,
		Values: map[string]any{"envelope": raw, "target_group": d.group, "replayed_from": dlqID},
	}).Err(); err != nil {
		return fmt.Errorf("re-publishing: %w", err)
	}
	return d.client.XDel(ctx, d.name(), dlqID).Err()
}

// Discard permanently drops a DLQ entry (after an operator decided it is not
// worth replaying).
func (d *DLQ) Discard(ctx context.Context, dlqID string) error {
	return d.client.XDel(ctx, d.name(), dlqID).Err()
}
