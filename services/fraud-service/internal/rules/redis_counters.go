package rules

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCounters keeps the sliding-ish windows as fixed buckets: a per-minute
// request counter and a per-UTC-day amount total, both expiring on their own
// so there is nothing to clean up. One pipelined round trip per check.
type RedisCounters struct{ client *redis.Client }

func NewRedisCounters(c *redis.Client) *RedisCounters { return &RedisCounters{client: c} }

func (r *RedisCounters) Record(ctx context.Context, accountID string, amountMinor int64, now time.Time) (int64, int64, error) {
	now = now.UTC()
	velKey := fmt.Sprintf("fraud:vel:%s:%d", accountID, now.Unix()/60)
	dayKey := fmt.Sprintf("fraud:day:%s:%s", accountID, now.Format("20060102"))

	pipe := r.client.Pipeline()
	vel := pipe.Incr(ctx, velKey)
	pipe.Expire(ctx, velKey, 2*time.Minute)
	day := pipe.IncrBy(ctx, dayKey, amountMinor)
	pipe.Expire(ctx, dayKey, 48*time.Hour)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, 0, err
	}
	return vel.Val(), day.Val(), nil
}
