// Package cache provides a minimal key/value cache abstraction with a Redis
// backend and a no-op fallback, so callers can degrade gracefully to their
// source of truth (the database) whenever Redis is not configured or reachable.
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache is a minimal byte-oriented cache. Implementations must be safe for
// concurrent use. Callers may treat any returned error as a cache miss and fall
// through to the authoritative source.
type Cache interface {
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// Noop stores nothing; every Get is a miss. Used when Redis is unavailable so
// callers run unchanged, going straight to the database.
type Noop struct{}

func (Noop) Get(context.Context, string) ([]byte, bool, error)        { return nil, false, nil }
func (Noop) Set(context.Context, string, []byte, time.Duration) error { return nil }
func (Noop) Delete(context.Context, string) error                     { return nil }

type redisCache struct {
	client *redis.Client
}

func (c *redisCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	b, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func (c *redisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

func (c *redisCache) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}

// NewRedis parses a redis:// URL, connects, and pings to verify reachability. It
// returns the cache and a close function. On any error the caller should fall
// back to Noop{}.
func NewRedis(ctx context.Context, url string) (Cache, func() error, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, nil, err
	}
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	return &redisCache{client: client}, client.Close, nil
}
