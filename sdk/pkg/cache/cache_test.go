package cache

import (
	"context"
	"testing"
	"time"
)

func TestNoopIsAlwaysMiss(t *testing.T) {
	var c Cache = Noop{}
	if err := c.Set(context.Background(), "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, found, err := c.Get(context.Background(), "k"); err != nil || found {
		t.Fatalf("Noop.Get should always miss, got found=%v err=%v", found, err)
	}
}

// TestRedisRoundTrip exercises the real Redis backend. It skips when Redis is
// not reachable so the suite stays green in environments without it.
func TestRedisRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c, closeFn, err := NewRedis(ctx, "redis://localhost:6379/0")
	if err != nil {
		t.Skipf("Redis not reachable, skipping: %v", err)
	}
	defer func() { _ = closeFn() }()

	key := "cache_test:roundtrip"
	_ = c.Delete(ctx, key)

	if _, found, _ := c.Get(ctx, key); found {
		t.Fatal("expected miss before set")
	}
	if err := c.Set(ctx, key, []byte("hello"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, found, err := c.Get(ctx, key)
	if err != nil || !found || string(v) != "hello" {
		t.Fatalf("Get after Set: v=%q found=%v err=%v", v, found, err)
	}
	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, found, _ := c.Get(ctx, key); found {
		t.Fatal("expected miss after delete")
	}
}
