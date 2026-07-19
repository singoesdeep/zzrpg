// Package eventstream fans domain events across nodes over Redis Streams. On the
// producing node the outbox relay (or any producer) forwards each event to the
// stream via a Publisher, stamped with the node's origin id. Every node runs a
// Consumer with its own consumer group, so each node receives every event
// (broadcast fan-out) and re-injects other nodes' events onto its local bus;
// events a node produced itself are skipped, since they were already delivered
// locally. Decoding reuses the outbox decoder registry.
//
// This is the horizontal-scale seam: with Redis present, in-proc subscribers on
// any node observe events produced on any other node. Without Redis the app runs
// single-node exactly as before (the publisher/consumer are simply not wired).
package eventstream

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/outbox"
)

// DefaultStream is the Redis stream key used when none is given.
const DefaultStream = "zzrpg:events"

// Dial connects a dedicated Redis client for event streaming and verifies
// reachability. The caller owns the returned client (close it on shutdown).
func Dial(ctx context.Context, url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// Publisher appends events to the stream, stamped with this node's origin.
type Publisher struct {
	client *redis.Client
	stream string
	origin string
}

// NewPublisher builds a publisher for the given stream (DefaultStream if empty).
func NewPublisher(client *redis.Client, stream, origin string) *Publisher {
	if stream == "" {
		stream = DefaultStream
	}
	return &Publisher{client: client, stream: stream, origin: origin}
}

// Publish encodes ev and XADDs it to the stream. It is best-effort fan-out: the
// event was already delivered to local subscribers, so callers typically log any
// error rather than failing the operation.
func (p *Publisher) Publish(ctx context.Context, ev bus.Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: p.stream,
		Values: map[string]any{"type": ev.Name(), "payload": payload, "origin": p.origin},
	}).Err()
}

// Consumer reads the stream via a per-node consumer group and re-injects other
// nodes' events onto the local bus.
type Consumer struct {
	client *redis.Client
	bus    bus.EventBus
	reg    *outbox.Registry
	log    *slog.Logger
	stream string
	group  string
	origin string
}

// NewConsumer builds a consumer whose group is unique to this node (so it
// receives every event). stream defaults to DefaultStream; log defaults to
// slog.Default().
func NewConsumer(client *redis.Client, b bus.EventBus, reg *outbox.Registry, stream, origin string, log *slog.Logger) *Consumer {
	if stream == "" {
		stream = DefaultStream
	}
	if log == nil {
		log = slog.Default()
	}
	return &Consumer{
		client: client,
		bus:    b,
		reg:    reg,
		log:    log,
		stream: stream,
		group:  "node:" + origin,
		origin: origin,
	}
}

// ensureGroup creates the consumer group at the stream tail (idempotent).
func (c *Consumer) ensureGroup(ctx context.Context) error {
	err := c.client.XGroupCreateMkStream(ctx, c.stream, c.group, "$").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

// Run consumes and dispatches until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) {
	if err := c.ensureGroup(ctx); err != nil {
		c.log.Error("eventstream: create group failed", "error", err)
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		res, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    c.group,
			Consumer: c.origin,
			Streams:  []string{c.stream, ">"},
			Count:    100,
			Block:    time.Second,
		}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) || ctx.Err() != nil {
				continue // block timed out with no messages
			}
			c.log.Error("eventstream: read failed", "error", err)
			time.Sleep(time.Second)
			continue
		}
		for _, st := range res {
			for _, msg := range st.Messages {
				c.handle(ctx, msg)
				c.client.XAck(ctx, c.stream, c.group, msg.ID)
			}
		}
	}
}

// handle decodes a message and republishes it locally, skipping events this node
// produced (already delivered locally) and those with no registered decoder.
func (c *Consumer) handle(ctx context.Context, msg redis.XMessage) {
	if origin, _ := msg.Values["origin"].(string); origin == c.origin {
		return
	}
	name, _ := msg.Values["type"].(string)
	payload, _ := msg.Values["payload"].(string)

	ev, ok, err := c.reg.Decode(name, []byte(payload))
	if err != nil {
		c.log.Error("eventstream: decode failed", "event", name, "error", err)
		return
	}
	if !ok {
		c.log.Warn("eventstream: no decoder registered", "event", name)
		return
	}
	_ = c.bus.Publish(ctx, ev)
}
