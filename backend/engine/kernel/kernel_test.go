package kernel

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/pkg/config"
)

// recorder is shared across fake plugins to capture lifecycle call order.
type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, s)
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	return out
}

type fakePlugin struct {
	name     string
	requires []string
	rec      *recorder
}

func (p *fakePlugin) Meta() plugin.Meta {
	return plugin.Meta{Name: p.name, Requires: p.requires}
}
func (p *fakePlugin) Init(plugin.InitContext) error { p.rec.add("init:" + p.name); return nil }
func (p *fakePlugin) Start(plugin.RunContext) error { p.rec.add("start:" + p.name); return nil }
func (p *fakePlugin) Stop(context.Context) error    { p.rec.add("stop:" + p.name); return nil }

func testKernel(plugins ...plugin.Plugin) *Kernel {
	cfg := &config.Config{Port: "0", Env: "test"}
	k := New(cfg, slog.New(slog.NewTextHandler(discard{}, nil)))
	k.Register(plugins...)
	return k
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func indexOf(calls []string, want string) int {
	for i, c := range calls {
		if c == want {
			return i
		}
	}
	return -1
}

// TestLifecycleOrder verifies Init/Start run in dependency order and Stop runs
// in reverse, using a pre-cancelled context so Run returns promptly.
func TestLifecycleOrder(t *testing.T) {
	rec := &recorder{}
	// b requires a; c requires b. Registered out of order on purpose.
	k := testKernel(
		&fakePlugin{name: "c", requires: []string{"b"}, rec: rec},
		&fakePlugin{name: "a", rec: rec},
		&fakePlugin{name: "b", requires: []string{"a"}, rec: rec},
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // trigger graceful shutdown immediately after Start

	done := make(chan error, 1)
	go func() { done <- k.Run(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within timeout")
	}

	calls := rec.snapshot()

	// Init order respects deps: a before b before c.
	if !(indexOf(calls, "init:a") < indexOf(calls, "init:b") && indexOf(calls, "init:b") < indexOf(calls, "init:c")) {
		t.Fatalf("init order violates dependencies: %v", calls)
	}
	// Start runs after all Init.
	if indexOf(calls, "start:a") < indexOf(calls, "init:c") {
		t.Fatalf("a started before all plugins initialised: %v", calls)
	}
	// Stop runs in reverse order: c before b before a.
	if !(indexOf(calls, "stop:c") < indexOf(calls, "stop:b") && indexOf(calls, "stop:b") < indexOf(calls, "stop:a")) {
		t.Fatalf("stop order not reversed: %v", calls)
	}
}

func TestMissingDependency(t *testing.T) {
	rec := &recorder{}
	k := testKernel(&fakePlugin{name: "combat", requires: []string{"character"}, rec: rec})

	err := k.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unknown plugin") {
		t.Fatalf("expected unknown-plugin error, got %v", err)
	}
	if len(rec.snapshot()) != 0 {
		t.Fatalf("no plugin should have run: %v", rec.snapshot())
	}
}

func TestDependencyCycle(t *testing.T) {
	rec := &recorder{}
	k := testKernel(
		&fakePlugin{name: "x", requires: []string{"y"}, rec: rec},
		&fakePlugin{name: "y", requires: []string{"x"}, rec: rec},
	)

	err := k.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestDuplicateName(t *testing.T) {
	rec := &recorder{}
	k := testKernel(
		&fakePlugin{name: "dup", rec: rec},
		&fakePlugin{name: "dup", rec: rec},
	)

	err := k.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate-name error, got %v", err)
	}
}
