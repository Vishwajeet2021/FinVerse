package eventbus

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// These tests run against a real Redis (set REDIS_TEST_ADDR, e.g.
// `docker run -p 6390:6379 redis:7-alpine` + REDIS_TEST_ADDR=localhost:6390):
// the retry/DLQ semantics live in Redis's pending-entry list, so a fake would
// only test the fake.
func testClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set")
	}
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis unreachable: %v", err)
	}
	return c
}

func newTestConsumer(t *testing.T, c *redis.Client, stream, group string) *Consumer {
	return NewConsumer(c, stream, group, uuid.NewString(), zap.NewNop(),
		WithBackoff(50*time.Millisecond, 200*time.Millisecond), WithMaxDeliveries(3))
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	c := NewConsumer(nil, "s", "g", "c", zap.NewNop(), WithBackoff(time.Second, 5*time.Second))
	want := map[int64]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 4: 5 * time.Second, 9: 5 * time.Second}
	for attempt, d := range want {
		if got := c.backoffFor(attempt); got != d {
			t.Errorf("backoffFor(%d) = %s, want %s", attempt, got, d)
		}
	}
}

func TestPersistentFailureIsRetriedThenDeadLetteredWithPayload(t *testing.T) {
	rc := testClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := "TestEvt-" + uuid.NewString()

	var calls atomic.Int64
	cons := newTestConsumer(t, rc, stream, "notification-service")
	go func() {
		_ = cons.Run(ctx, func(context.Context, Envelope) error {
			calls.Add(1)
			return errors.New("email provider down")
		})
	}()

	if err := NewPublisher(rc).Publish(ctx, "test", stream, "", map[string]string{"k": "v"}); err != nil {
		t.Fatal(err)
	}

	dlq := cons.DLQ()
	waitFor(t, "message in DLQ", func() bool { n, _ := dlq.Len(ctx); return n == 1 })

	if got := calls.Load(); got != 3 {
		t.Errorf("handler called %d times, want exactly MaxDeliveries=3", got)
	}
	entries, err := dlq.List(ctx, 10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("List: %v, %d entries", err, len(entries))
	}
	e := entries[0]
	if e.Envelope == nil || string(e.Envelope.Data) != `{"k":"v"}` {
		t.Errorf("DLQ entry lost the original payload: %+v", e.Envelope)
	}
	if e.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", e.Attempts)
	}
	if cons.Stats().DeadLettered != 1 {
		t.Errorf("stats = %+v", cons.Stats())
	}
	// Dead-lettered messages must leave the pending list, or they'd be re-evaluated forever.
	if p, _ := rc.XPending(ctx, stream, "notification-service").Result(); p.Count != 0 {
		t.Errorf("pending count = %d after dead-lettering, want 0", p.Count)
	}
}

func TestPermanentErrorSkipsRetries(t *testing.T) {
	rc := testClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := "TestEvt-" + uuid.NewString()

	var calls atomic.Int64
	cons := newTestConsumer(t, rc, stream, "g")
	go func() {
		_ = cons.Run(ctx, func(context.Context, Envelope) error {
			calls.Add(1)
			return Permanent(errors.New("malformed payload"))
		})
	}()
	_ = NewPublisher(rc).Publish(ctx, "test", stream, "", map[string]string{})

	waitFor(t, "message in DLQ", func() bool { n, _ := cons.DLQ().Len(ctx); return n == 1 })
	if calls.Load() != 1 {
		t.Errorf("handler called %d times for a permanent error, want 1", calls.Load())
	}
}

func TestTransientFailureRecoversWithoutDLQ(t *testing.T) {
	rc := testClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := "TestEvt-" + uuid.NewString()

	var calls atomic.Int64
	cons := newTestConsumer(t, rc, stream, "g")
	go func() {
		_ = cons.Run(ctx, func(context.Context, Envelope) error {
			if calls.Add(1) < 3 {
				return errors.New("blip")
			}
			return nil
		})
	}()
	_ = NewPublisher(rc).Publish(ctx, "test", stream, "", map[string]string{})

	waitFor(t, "eventual success", func() bool { return cons.Stats().Processed == 1 })
	if n, _ := cons.DLQ().Len(ctx); n != 0 {
		t.Errorf("DLQ has %d entries after a recovered failure", n)
	}
}

func TestReplayRedeliversOnlyToTargetGroup(t *testing.T) {
	rc := testClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := "TestEvt-" + uuid.NewString()

	var healthy atomic.Int64
	var fixed atomic.Bool
	var failing atomic.Int64

	other := newTestConsumer(t, rc, stream, "audit-service")
	go func() {
		_ = other.Run(ctx, func(context.Context, Envelope) error { healthy.Add(1); return nil })
	}()
	broken := newTestConsumer(t, rc, stream, "notification-service")
	go func() {
		_ = broken.Run(ctx, func(context.Context, Envelope) error {
			if !fixed.Load() {
				return errors.New("down")
			}
			failing.Add(1)
			return nil
		})
	}()

	_ = NewPublisher(rc).Publish(ctx, "test", stream, "", map[string]string{"n": "1"})
	waitFor(t, "audit processed", func() bool { return healthy.Load() == 1 })
	waitFor(t, "notification dead-lettered", func() bool { n, _ := broken.DLQ().Len(ctx); return n == 1 })

	// The outage is fixed; an operator replays the dead letter.
	fixed.Store(true)
	entries, _ := broken.DLQ().List(ctx, 10)
	if err := broken.DLQ().Replay(ctx, entries[0].ID); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "notification recovered", func() bool { return failing.Load() == 1 })
	waitFor(t, "audit skipped the replay", func() bool { return other.Stats().SkippedTarget == 1 })
	if healthy.Load() != 1 {
		t.Errorf("audit-service handled the event %d times, want 1 — replay leaked to another group", healthy.Load())
	}
	if n, _ := broken.DLQ().Len(ctx); n != 0 {
		t.Errorf("DLQ not drained after replay: %d", n)
	}
}
