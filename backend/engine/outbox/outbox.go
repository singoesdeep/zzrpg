// Package outbox implements the transactional outbox pattern on top of
// engine/store. A domain event is written to the outbox table in the SAME
// transaction as the state change that produced it (via Append, using the
// transaction's Querier), so the event and the state can never diverge — if the
// commit succeeds the event is durably recorded, if it rolls back the event
// vanishes with it. A Relay then dispatches undispatched rows onto the in-process
// bus after commit (at-least-once), decoding each row back to its typed event
// through a registered decoder.
//
// This is the single-node foundation: durable, atomic, at-least-once delivery of
// DB-tied events. Multi-node fan-out (Redis Streams consumer groups, DLQ) and
// event_log replay build on this seam later.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/store"
)

// Append writes ev to the outbox using q — which must be the Querier of the
// transaction performing the state change, so the event enlists in that
// transaction. The event is stored as its Name() plus its JSON encoding.
func Append(ctx context.Context, q store.Querier, ev bus.Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal outbox event %q: %w", ev.Name(), err)
	}
	_, err = q.Exec(ctx,
		`INSERT INTO outbox (event_type, payload) VALUES ($1, $2)`,
		ev.Name(), payload,
	)
	if err != nil {
		return fmt.Errorf("append outbox event %q: %w", ev.Name(), err)
	}
	return nil
}

// Decoder reconstructs a typed bus.Event from a stored payload so the relay can
// republish it exactly as the producer emitted it.
type Decoder func(payload []byte) (bus.Event, error)

// JSONDecoder builds a Decoder that unmarshals the payload into T. Use it to
// register a domain event type: Register(Ev{}.Name(), JSONDecoder[Ev]()).
func JSONDecoder[T bus.Event]() Decoder {
	return func(payload []byte) (bus.Event, error) {
		var ev T
		if err := json.Unmarshal(payload, &ev); err != nil {
			return nil, err
		}
		return ev, nil
	}
}
