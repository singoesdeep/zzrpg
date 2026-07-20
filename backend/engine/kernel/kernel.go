// Package kernel is the game-agnostic engine core. It owns configuration, the
// logger, the service registry, the event bus, and the HTTP router, and drives
// the plugin lifecycle: topologically sorted Init, then Start, serve HTTP until
// the context is cancelled, then Stop in reverse order. It contains zero RPG
// concepts.
package kernel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/singoesdeep/zzrpg/backend/engine/admin"
	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/hooks"
	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/pkg/config"
	"github.com/singoesdeep/zzrpg/backend/pkg/httpx"
	"github.com/singoesdeep/zzrpg/backend/pkg/metrics"
)

// Kernel wires the engine primitives together and runs plugins.
type Kernel struct {
	cfg     *config.Config
	log     *slog.Logger
	reg     *registry.Registry
	bus     bus.EventBus
	hooks   *hooks.Hooks
	mux     *http.ServeMux
	metrics *metrics.Metrics
	plugins []plugin.Plugin
}

// New builds a kernel with a fresh registry, in-proc event bus, HTTP mux, and
// Prometheus metrics.
func New(cfg *config.Config, log *slog.Logger) *Kernel {
	return &Kernel{
		cfg: cfg,
		log: log,
		reg: registry.New(),
		// Fanout-wrapped so events can additionally be broadcast to other nodes
		// when a forwarder is installed; a transparent pass-through until then.
		bus:     bus.NewFanout(bus.NewInProc(log)),
		hooks:   hooks.New(log),
		mux:     http.NewServeMux(),
		metrics: metrics.New(),
	}
}

// Register adds one or more plugins to the kernel and returns it for chaining.
func (k *Kernel) Register(plugins ...plugin.Plugin) *Kernel {
	k.plugins = append(k.plugins, plugins...)
	return k
}

// Bus exposes the kernel's event bus (e.g. so a legacy facade can share it).
func (k *Kernel) Bus() bus.EventBus { return k.bus }

// Registry exposes the kernel's service registry.
func (k *Kernel) Registry() *registry.Registry { return k.reg }

// Run resolves the plugin dependency graph, runs Init then Start in order,
// serves HTTP until ctx is cancelled, then Stops plugins in reverse order and
// shuts the server down gracefully.
func (k *Kernel) Run(ctx context.Context) error {
	ordered, err := topoSort(k.plugins)
	if err != nil {
		return err
	}

	// Metrics endpoint + registry access for plugins (so domain collectors can be
	// registered during Init).
	k.mux.Handle("GET /metrics", k.metrics.Handler())
	if err := registry.Provide(k.reg, "metrics", k.metrics); err != nil {
		return err
	}

	var catalog []admin.PluginState
	for _, p := range ordered {
		info := admin.PluginState{
			Name:     p.Meta().Name,
			Requires: p.Meta().Requires,
			Status:   admin.StatusActive,
		}
		if desc, ok := p.(admin.Describor); ok {
			adm := desc.AdminInfo()
			info.Admin = &adm
		}
		catalog = append(catalog, info)
	}
	// StateManager is the single source of truth for plugin activation state.
	mgr := admin.NewStateManager(catalog)
	if err := registry.Provide(k.reg, "pluginManager", mgr); err != nil {
		return err
	}

	// Collect plugin-owned schema before Init so the persistence plugin can apply
	// core + plugin migrations together when it initialises.
	var migrations []plugin.MigrationSource
	for _, p := range ordered {
		if m, ok := p.(plugin.Migrator); ok {
			migrations = append(migrations, m.Migrations())
		}
	}
	if err := registry.Provide(k.reg, "pluginMigrations", migrations); err != nil {
		return err
	}

	for _, p := range ordered {
		ic := &engineContext{ctx: ctx, k: k, name: p.Meta().Name, mgr: mgr}
		if err := p.Init(ic); err != nil {
			return fmt.Errorf("plugin %q init: %w", p.Meta().Name, err)
		}
		k.log.Info("plugin initialised", "plugin", p.Meta().Name)
	}

	for _, p := range ordered {
		rc := &engineContext{ctx: ctx, k: k, name: p.Meta().Name, mgr: mgr}
		if err := p.Start(rc); err != nil {
			return fmt.Errorf("plugin %q start: %w", p.Meta().Name, err)
		}
	}

	// Middleware chain, outermost first: recover from panics, assign a request
	// id, log the request, set security headers, apply CORS, rate-limit per
	// client IP, and cap the request body — then the router.
	var handler http.Handler = k.mux
	handler = httpx.MaxBodyBytes(k.cfg.MaxBodyBytes)(handler)
	handler = httpx.RateLimit(k.cfg.RateLimitRPS, k.cfg.RateLimitBurst, k.log)(handler)
	handler = httpx.CORS(k.cfg.AllowOrigin)(handler)
	handler = httpx.SecureHeaders(handler)
	handler = k.metrics.Middleware(handler)
	handler = httpx.RequestLogger(k.log)(handler)
	handler = httpx.RequestID(handler)
	handler = httpx.Recover(k.log)(handler)
	srv := &http.Server{
		Addr:         ":" + k.cfg.Port,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		k.log.Info("HTTP server listening", "address", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case err := <-serveErr:
		// Listen failed outright; tear down and report.
		k.stopAll(ordered)
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		k.log.Info("Shutdown signal received, shutting down server...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		k.log.Error("Server forced to shutdown", "error", err)
	}

	k.stopAll(ordered)
	k.log.Info("zzrpg backend stopped gracefully")
	return nil
}

// stopAll stops plugins in reverse order, logging (but not aborting on) errors.
func (k *Kernel) stopAll(ordered []plugin.Plugin) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := len(ordered) - 1; i >= 0; i-- {
		if err := ordered[i].Stop(shutdownCtx); err != nil {
			k.log.Error("plugin stop failed", "plugin", ordered[i].Meta().Name, "error", err)
		}
	}
}

// engineContext implements both plugin.InitContext and plugin.RunContext by
// delegating to the kernel. It is scoped to a single plugin (name) so the
// HTTP router and event bus it hands out gate on that plugin's activation.
type engineContext struct {
	ctx  context.Context
	k    *Kernel
	name string
	mgr  *admin.StateManager
}

func (e *engineContext) Context() context.Context     { return e.ctx }
func (e *engineContext) Logger() *slog.Logger         { return e.k.log }
func (e *engineContext) Config() *config.Config       { return e.k.cfg }
func (e *engineContext) Registry() *registry.Registry { return e.k.reg }
func (e *engineContext) Bus() bus.EventBus {
	return &gatedBus{inner: e.k.bus, name: e.name, mgr: e.mgr}
}
func (e *engineContext) Hooks() *hooks.Hooks { return e.k.hooks }
func (e *engineContext) Mux() plugin.Router {
	return &gatedRouter{mux: e.k.mux, name: e.name, mgr: e.mgr}
}

// gatedRouter registers routes on the shared mux but wraps each handler so that
// requests are rejected with 503 while the owning plugin is deactivated.
type gatedRouter struct {
	mux  *http.ServeMux
	name string
	mgr  *admin.StateManager
}

func (g *gatedRouter) Handle(pattern string, h http.Handler) {
	g.mux.Handle(pattern, g.gate(h))
}

func (g *gatedRouter) HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	g.mux.Handle(pattern, g.gate(http.HandlerFunc(h)))
}

func (g *gatedRouter) gate(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.mgr.IsActive(g.name) {
			http.Error(w, "plugin deactivated", http.StatusServiceUnavailable)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// gatedBus is a plugin-scoped EventBus: Publish passes through unchanged, but
// subscriptions are suppressed while the owning plugin is deactivated so a
// disabled plugin stops reacting to events without being unsubscribed.
type gatedBus struct {
	inner bus.EventBus
	name  string
	mgr   *admin.StateManager
}

func (g *gatedBus) Publish(ctx context.Context, ev bus.Event) error {
	return g.inner.Publish(ctx, ev)
}

func (g *gatedBus) Subscribe(name string, h bus.Handler) bus.Subscription {
	return g.inner.Subscribe(name, func(ctx context.Context, ev bus.Event) {
		if !g.mgr.IsActive(g.name) {
			return
		}
		h(ctx, ev)
	})
}

// topoSort orders plugins so that every plugin appears after all plugins it
// Requires. It fails fast on an unknown dependency name or a dependency cycle.
func topoSort(plugins []plugin.Plugin) ([]plugin.Plugin, error) {
	byName := make(map[string]plugin.Plugin, len(plugins))
	for _, p := range plugins {
		name := p.Meta().Name
		if _, dup := byName[name]; dup {
			return nil, fmt.Errorf("duplicate plugin name %q", name)
		}
		byName[name] = p
	}

	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := make(map[string]int, len(plugins))
	ordered := make([]plugin.Plugin, 0, len(plugins))

	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		switch state[name] {
		case done:
			return nil
		case visiting:
			return fmt.Errorf("plugin dependency cycle: %v -> %s", path, name)
		}
		state[name] = visiting
		p := byName[name]
		for _, dep := range p.Meta().Requires {
			if _, ok := byName[dep]; !ok {
				return fmt.Errorf("plugin %q requires unknown plugin %q", name, dep)
			}
			if err := visit(dep, append(path, name)); err != nil {
				return err
			}
		}
		state[name] = done
		ordered = append(ordered, p)
		return nil
	}

	for _, p := range plugins {
		if err := visit(p.Meta().Name, nil); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}
