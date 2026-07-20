package outbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/singoesdeep/zzrpg/sdk/engine/bus"
	"github.com/singoesdeep/zzrpg/sdk/engine/store"
)

// Relay dispatches undispatched outbox rows onto the bus after commit. It is the
// single-node dispatcher: it polls, decodes each row to its typed event, and
// publishes it, marking the row published. Delivery is at-least-once (a crash
// between publish and mark re-delivers on the next scan).
type Relay struct {
	store store.Store
	bus   bus.EventBus
	log   *slog.Logger
	reg   *Registry
	batch int
}

// NewRelay builds a relay over the given store and bus. If log is nil,
// slog.Default() is used.
func NewRelay(st store.Store, b bus.EventBus, log *slog.Logger) *Relay {
	if log == nil {
		log = slog.Default()
	}
	return &Relay{
		store: st,
		bus:   b,
		log:   log,
		reg:   NewRegistry(),
		batch: 100,
	}
}

// Register associates an event type name with a decoder so the relay can rebuild
// the typed event before publishing. Producers register their outbox event types
// at startup.
func (r *Relay) Register(name string, d Decoder) { r.reg.Register(name, d) }

// Registry returns the relay's decoder registry so other consumers (e.g. the
// cross-node event stream) can reuse the same registrations.
func (r *Relay) Registry() *Registry { return r.reg }

// Dispatch runs one poll cycle: it publishes every currently-undispatched row in
// insertion order and marks it published, returning the number dispatched. A row
// whose type has no registered decoder (or fails to decode) is logged and marked
// published so it cannot block the queue — DLQ handling is a later addition.
func (r *Relay) Dispatch(ctx context.Context) (int, error) {
	rows, err := r.store.Query(ctx,
		`SELECT id, event_type, payload FROM outbox WHERE published_at IS NULL ORDER BY id LIMIT $1`,
		r.batch,
	)
	if err != nil {
		return 0, err
	}

	type record struct {
		id        int64
		eventType string
		payload   []byte
	}
	var records []record
	for rows.Next() {
		var rec record
		if err := rows.Scan(&rec.id, &rec.eventType, &rec.payload); err != nil {
			rows.Close()
			return 0, err
		}
		records = append(records, rec)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var dispatched int
	for _, rec := range records {
		ev, ok, derr := r.reg.Decode(rec.eventType, rec.payload)
		switch {
		case derr != nil:
			r.log.Error("outbox: decode failed, skipping", "event", rec.eventType, "id", rec.id, "error", derr)
		case !ok:
			r.log.Warn("outbox: no decoder registered, skipping", "event", rec.eventType, "id", rec.id)
		default:
			// Publishing on the (fanout) bus both delivers locally and, when a
			// forwarder is installed, broadcasts to other nodes.
			_ = r.bus.Publish(ctx, ev)
		}

		if _, err := r.store.Exec(ctx, `UPDATE outbox SET published_at = now() WHERE id = $1`, rec.id); err != nil {
			return dispatched, err
		}
		dispatched++
	}
	return dispatched, nil
}

// Prune deletes dispatched (published) outbox rows older than retention, keeping
// the table from growing without bound. Undispatched rows are never removed. It
// returns the number of rows deleted.
func (r *Relay) Prune(ctx context.Context, retention time.Duration) (int64, error) {
	tag, err := r.store.Exec(ctx,
		`DELETE FROM outbox WHERE published_at IS NOT NULL AND published_at < now() - make_interval(secs => $1)`,
		retention.Seconds(),
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// RunPruner prunes dispatched rows older than retention every interval until ctx
// is cancelled. retention <= 0 disables pruning (returns immediately).
func (r *Relay) RunPruner(ctx context.Context, interval, retention time.Duration) {
	if retention <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := r.Prune(ctx, retention)
			if err != nil {
				r.log.Error("outbox: prune failed", "error", err)
				continue
			}
			if n > 0 {
				r.log.Info("outbox: pruned dispatched rows", "count", n)
			}
		}
	}
}

// Run polls at interval until ctx is cancelled. Dispatch errors are logged and
// retried on the next tick.
func (r *Relay) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := r.Dispatch(ctx); err != nil {
				r.log.Error("outbox: dispatch cycle failed", "error", err)
			}
		}
	}
}
