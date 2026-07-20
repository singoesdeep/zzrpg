// Package plugin defines the contract every engine plugin implements. A plugin
// bundles a slice of game (or infrastructure) behaviour: it registers services
// and content into the registry, wires HTTP routes, subscribes to events, and
// optionally runs background work. The kernel drives plugins through an ordered
// lifecycle (Init -> Start -> Stop) so that construction, wiring, and teardown
// are declarative rather than hand-written in main().
package plugin

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/hooks"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/pkg/config"
)

// Plugin is the unit of composition the kernel loads. Implementations are
// registered with Kernel.Register and run in dependency order.
type Plugin interface {
	// Meta returns the plugin's identity and hard dependencies (by name).
	Meta() Meta
	// Init registers services, content, routes, and event subscriptions. When
	// Init runs, every plugin named in Meta().Requires has already been
	// initialised, so their services are resolvable from the registry.
	Init(InitContext) error
	// Start begins background work (e.g. a hub loop). When Start runs, every
	// plugin has completed Init, so all services are present in the registry.
	Start(RunContext) error
	// Stop tears the plugin down. The kernel calls Stop in reverse start order.
	Stop(context.Context) error
}

// Meta is a plugin's identity and hard-dependency declaration.
type Meta struct {
	// Name uniquely identifies the plugin; used for dependency ordering.
	Name string
	// Requires lists the names of plugins that must Init before this one. The
	// kernel topologically sorts on these and fails fast on missing deps/cycles.
	Requires []string
}

// InitContext is a plugin's sole channel to the engine during Init.
type InitContext interface {
	Context() context.Context
	Logger() *slog.Logger
	Config() *config.Config
	Registry() *registry.Registry
	Bus() bus.EventBus
	// Hooks is the synchronous extension registry: plugins add filters (transform
	// a value mid-flow) and actions (ordered side effects / gates) here during Init.
	Hooks() *hooks.Hooks
	// Mux is the shared HTTP router the kernel serves. Plugins register their
	// routes on it during Init.
	Mux() *http.ServeMux
}

// RunContext is passed to Start. By the time Start runs all services are
// registered, so cross-plugin dependencies can be resolved here into fields.
type RunContext interface {
	Context() context.Context
	Logger() *slog.Logger
	Registry() *registry.Registry
	Bus() bus.EventBus
}

// Base provides no-op Start/Stop so plugins that only need Init can embed it
// and implement just Meta and Init.
type Base struct{}

func (Base) Start(RunContext) error     { return nil }
func (Base) Stop(context.Context) error { return nil }
