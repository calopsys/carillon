package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// keyPrefix namespaces every high-water-mark key so a shared Redis stays tidy.
const keyPrefix = "carillon:hwm:"

// Redis is a Redis-backed Store. Keys carry no TTL, so the instance must be
// persistent (AOF/RDB) and configured noeviction — otherwise an evicted key
// re-baselines that tracker and re-notifies its current latest on the next run.
type Redis struct {
	client *redis.Client
}

var _ Store = (*Redis)(nil)

// OpenRedis parses a redis:// or rediss:// URL, connects, and verifies
// connectivity. The URL carries host, optional auth, DB index, and TLS.
func OpenRedis(ctx context.Context, url string) (*Redis, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	client := redis.NewClient(opt)
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return &Redis{client: client}, nil
}

func (r *Redis) Close() { _ = r.client.Close() }

func (r *Redis) Get(ctx context.Context, name string) (Mark, bool, error) {
	raw, err := r.client.Get(ctx, keyPrefix+name).Bytes()
	if errors.Is(err, redis.Nil) {
		return Mark{}, false, nil
	}
	if err != nil {
		return Mark{}, false, fmt.Errorf("get mark %q: %w", name, err)
	}
	m, err := unmarshalMark(raw)
	if err != nil {
		return Mark{}, false, fmt.Errorf("decode mark %q: %w", name, err)
	}
	return m, true, nil
}

func (r *Redis) Upsert(ctx context.Context, m Mark) error {
	raw, err := marshalMark(m)
	if err != nil {
		return fmt.Errorf("encode mark %q: %w", m.Name, err)
	}
	// A plain SET (no TTL): the mark is a pure key→value high-water mark, so the
	// write is a single round-trip with no read-modify-write.
	if err := r.client.Set(ctx, keyPrefix+m.Name, raw, 0).Err(); err != nil {
		return fmt.Errorf("upsert mark %q: %w", m.Name, err)
	}
	return nil
}

// marshalMark / unmarshalMark are the pure JSON codec for a Mark, split out so
// the encoding is testable without a Redis server.
func marshalMark(m Mark) ([]byte, error) { return json.Marshal(m) }

func unmarshalMark(raw []byte) (Mark, error) {
	var m Mark
	err := json.Unmarshal(raw, &m)
	return m, err
}
