package outbox

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/store"
)

// --- fakes (no database needed) -------------------------------------------

type testEvent struct {
	CharacterID int64
	Amount      int64
}

func (testEvent) Name() string { return "test_event" }

// execCall records one Exec invocation.
type execCall struct {
	sql  string
	args []any
}

// fakeStore implements store.Store in memory: Query returns a queued fakeRows,
// Exec records the call. WithinTx just runs fn with the store itself.
type fakeStore struct {
	rows  *fakeRows
	execs []execCall
}

func (f *fakeStore) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return f.rows, nil
}
func (f *fakeStore) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row { return nil }
func (f *fakeStore) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execs = append(f.execs, execCall{sql: sql, args: args})
	return pgconn.CommandTag{}, nil
}
func (f *fakeStore) WithinTx(ctx context.Context, fn func(q store.Querier) error) error {
	return fn(f)
}

// fakeRows serves a fixed set of (id, event_type, payload) tuples.
type fakeRows struct {
	data [][]any
	i    int
}

func (r *fakeRows) Next() bool { r.i++; return r.i <= len(r.data) }
func (r *fakeRows) Scan(dest ...any) error {
	row := r.data[r.i-1]
	*dest[0].(*int64) = row[0].(int64)
	*dest[1].(*string) = row[1].(string)
	*dest[2].(*[]byte) = row[2].([]byte)
	return nil
}
func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return nil }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }

func TestAppendInsertsTypeAndPayload(t *testing.T) {
	f := &fakeStore{}
	ev := testEvent{CharacterID: 7, Amount: 42}
	if err := Append(context.Background(), f, ev); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if len(f.execs) != 1 {
		t.Fatalf("expected 1 insert, got %d", len(f.execs))
	}
	call := f.execs[0]
	if call.args[0] != "test_event" {
		t.Errorf("event_type: got %v, want test_event", call.args[0])
	}
	var got testEvent
	if err := json.Unmarshal(call.args[1].([]byte), &got); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if got != ev {
		t.Errorf("payload round-trip: got %+v, want %+v", got, ev)
	}
}

func TestRelayDispatchDecodesPublishesAndMarks(t *testing.T) {
	payload, _ := json.Marshal(testEvent{CharacterID: 7, Amount: 42})
	f := &fakeStore{rows: &fakeRows{data: [][]any{{int64(1), "test_event", payload}}}}

	eventBus := bus.NewInProc(nil)
	got := make(chan bus.Event, 1)
	eventBus.Subscribe("test_event", func(_ context.Context, ev bus.Event) { got <- ev })

	relay := NewRelay(f, eventBus, nil)
	relay.Register("test_event", JSONDecoder[testEvent]())

	n, err := relay.Dispatch(context.Background())
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if n != 1 {
		t.Fatalf("dispatched %d, want 1", n)
	}

	select {
	case ev := <-got:
		te, ok := ev.(testEvent)
		if !ok || te.CharacterID != 7 || te.Amount != 42 {
			t.Errorf("unexpected event published: %#v", ev)
		}
	default:
		// Publish is async; give the goroutine a beat via a blocking receive.
		ev := <-got
		if te, ok := ev.(testEvent); !ok || te.CharacterID != 7 {
			t.Errorf("unexpected event: %#v", ev)
		}
	}

	// The row must be marked published (one UPDATE with its id).
	var marked bool
	for _, c := range f.execs {
		if len(c.args) == 1 {
			if id, ok := c.args[0].(int64); ok && id == 1 {
				marked = true
			}
		}
	}
	if !marked {
		t.Errorf("expected the dispatched row to be marked published")
	}
}
