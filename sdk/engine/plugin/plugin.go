// Package plugin defines the contract every engine plugin implements. A plugin
// bundles a slice of game (or infrastructure) behaviour: it registers services
// and content into the registry, wires HTTP routes, subscribes to events, and
// optionally runs background work. The kernel drives plugins through an ordered
// lifecycle (Init -> Start -> Stop) so that construction, wiring, and teardown
// are declarative rather than hand-written in main().
package plugin

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/singoesdeep/zzrpg/sdk/engine/bus"
	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
	"github.com/singoesdeep/zzrpg/sdk/engine/registry"
	"github.com/singoesdeep/zzrpg/sdk/pkg/config"
)

// APIVersion is the version of the plugin extension contract (the Plugin
// interface, InitContext, the event bus, and the hook system). It follows
// semver: a minor bump adds extension points; a major bump is a breaking change
// to the surface plugins depend on. Plugin authors can pin against it.
const APIVersion = "0.1.0"

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

// Router is the subset of *http.ServeMux plugins use to register routes. The
// kernel hands plugins a plugin-scoped Router so that routes belonging to a
// deactivated plugin can be gated (returning 503) without the plugin having to
// check its own activation state.
type Router interface {
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// InitContext is a plugin's sole channel to the engine during Init.
type InitContext interface {
	Context() context.Context
	Logger() *slog.Logger
	Config() *config.Config
	Registry() *registry.Registry
	// Bus is a plugin-scoped event bus: subscriptions registered through it are
	// automatically suppressed while the plugin is deactivated.
	Bus() bus.EventBus
	// Hooks is the synchronous extension registry: plugins add filters (transform
	// a value mid-flow) and actions (ordered side effects / gates) here during Init.
	Hooks() *hooks.Hooks
	// Mux is the shared HTTP router the kernel serves. Plugins register their
	// routes on it during Init; the routes are gated on the plugin's activation.
	Mux() Router
}

// RunContext is passed to Start. By the time Start runs all services are
// registered, so cross-plugin dependencies can be resolved here into fields.
type RunContext interface {
	Context() context.Context
	Logger() *slog.Logger
	Registry() *registry.Registry
	Bus() bus.EventBus
}

// MigrationSource is a plugin's owned database schema: a filesystem of
// "NNNNNN_name.up.sql" files under Dir, namespaced by Module so a plugin's
// versions never collide with core or other plugins. It is a neutral type (no
// persistence-layer import) so the engine can carry it without a dependency
// cycle; the persistence plugin applies it.
type MigrationSource struct {
	Module string
	FS     fs.FS
	Dir    string
}

// Migrator is the optional interface a plugin implements to ship its own schema.
// The kernel collects every plugin's MigrationSource before Init and publishes
// them under the "pluginMigrations" service, so the persistence plugin can apply
// them alongside the core schema — no plugin needs to touch platform/database.
type Migrator interface {
	Migrations() MigrationSource
}

// Base provides no-op Start/Stop so plugins that only need Init can embed it
// and implement just Meta and Init.
type Base struct{}

func (Base) Start(RunContext) error     { return nil }
func (Base) Stop(context.Context) error { return nil }
