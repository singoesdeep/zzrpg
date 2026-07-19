// Package eventlog is an append-only, per-stream history of domain events, used
// for replay — e.g. catching a reconnecting character up on what happened while
// it was away. Unlike the outbox (a transient dispatch queue), event_log rows are
// never removed. Events are appended in the same transaction as the state change
// that produced them (via Append, using that transaction's Querier), and read
// back in order with Replay, decoded through the shared outbox.Registry.
package eventlog

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/outbox"
	"github.com/singoesdeep/zzrpg/backend/engine/store"
)

// CharacterStream is the stream key for a character's event history.
func CharacterStream(characterID int64) string {
	return "character:" + strconv.FormatInt(characterID, 10)
}

// Recorded is one replayed event plus when it happened.
type Recorded struct {
	Event      bus.Event
	OccurredAt time.Time
}

// Append writes ev to the given stream using q — which must be the Querier of the
// transaction performing the state change, so the log entry enlists in that
// transaction and can never diverge from the committed state.
func Append(ctx context.Context, q store.Querier, stream string, ev bus.Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event %q: %w", ev.Name(), err)
	}
	_, err = q.Exec(ctx,
		`INSERT INTO event_log (stream, event_type, payload) VALUES ($1, $2, $3)`,
		stream, ev.Name(), payload,
	)
	if err != nil {
		return fmt.Errorf("append event %q to %q: %w", ev.Name(), stream, err)
	}
	return nil
}

// Replay returns the events recorded for stream strictly after `since`, in
// insertion order, decoded via reg. Entries whose type has no registered decoder
// are skipped (they cannot be reconstructed).
func Replay(ctx context.Context, q store.Querier, reg *outbox.Registry, stream string, since time.Time) ([]Recorded, error) {
	rows, err := q.Query(ctx,
		`SELECT event_type, payload, occurred_at FROM event_log
		 WHERE stream = $1 AND occurred_at > $2 ORDER BY id`,
		stream, since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Recorded
	for rows.Next() {
		var eventType string
		var payload []byte
		var occurredAt time.Time
		if err := rows.Scan(&eventType, &payload, &occurredAt); err != nil {
			return nil, err
		}
		ev, ok, derr := reg.Decode(eventType, payload)
		if derr != nil || !ok {
			continue // unknown/undecodable type — skip
		}
		out = append(out, Recorded{Event: ev, OccurredAt: occurredAt})
	}
	return out, rows.Err()
}
