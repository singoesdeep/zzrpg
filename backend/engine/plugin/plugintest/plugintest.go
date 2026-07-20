// Package plugintest helps plugin authors test a plugin in isolation, without
// standing up the whole kernel. A Harness implements both plugin.InitContext and
// plugin.RunContext over real engine primitives (a hook registry, an in-proc bus,
// an HTTP mux, and a DI registry), so a test can:
//
//	h := plugintest.New()
//	registry.Provide(h.Registry(), "character", mockCharSvc) // satisfy deps
//	if err := h.Init(&myplugin.Plugin{}); err != nil { t.Fatal(err) }
//	// then inspect: apply the plugin's filters, publish to h.Bus(), serve h.Mux()...
//
// The primitives are the same types the kernel uses, so behaviour matches
// production. It mirrors the net/http/httptest pattern for plugins.
package plugintest

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/hooks"
	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/pkg/config"
)

// Harness satisfies both engine contexts.
var (
	_ plugin.InitContext = (*Harness)(nil)
	_ plugin.RunContext  = (*Harness)(nil)
)

// Harness is a test double for the engine context passed to a plugin. It
// satisfies plugin.InitContext and plugin.RunContext.
type Harness struct {
	ctx context.Context
	log *slog.Logger
	cfg *config.Config
	reg *registry.Registry
	bus bus.EventBus
	hks *hooks.Hooks
	mux *http.ServeMux
}

// Option customises a Harness.
type Option func(*Harness)

// WithConfig sets the config returned by Config().
func WithConfig(cfg *config.Config) Option { return func(h *Harness) { h.cfg = cfg } }

// WithContext sets the context returned by Context().
func WithContext(ctx context.Context) Option { return func(h *Harness) { h.ctx = ctx } }

// WithLogger sets the logger returned by Logger().
func WithLogger(log *slog.Logger) Option { return func(h *Harness) { h.log = log } }

// New builds a Harness with real, empty engine primitives. Pre-populate the
// registry (Provide the services a plugin resolves) and subscribe to the bus
// before calling Init.
func New(opts ...Option) *Harness {
	h := &Harness{
		ctx: context.Background(),
		log: slog.Default(),
		cfg: &config.Config{},
		reg: registry.New(),
		bus: bus.NewInProc(nil),
		hks: hooks.New(nil),
		mux: http.NewServeMux(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Init runs the plugin's Init against this harness.
func (h *Harness) Init(p plugin.Plugin) error { return p.Init(h) }

// Start runs the plugin's Start against this harness.
func (h *Harness) Start(p plugin.Plugin) error { return p.Start(h) }

// InitContext / RunContext implementation.
func (h *Harness) Context() context.Context     { return h.ctx }
func (h *Harness) Logger() *slog.Logger         { return h.log }
func (h *Harness) Config() *config.Config       { return h.cfg }
func (h *Harness) Registry() *registry.Registry { return h.reg }
func (h *Harness) Bus() bus.EventBus            { return h.bus }
func (h *Harness) Hooks() *hooks.Hooks          { return h.hks }
func (h *Harness) Mux() plugin.Router            { return h.mux }

// ServeMux exposes the concrete *http.ServeMux so tests can drive registered
// routes with httptest (Mux() returns the narrower plugin.Router interface).
func (h *Harness) ServeMux() *http.ServeMux { return h.mux }
