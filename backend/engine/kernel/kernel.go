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

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/pkg/config"
	"github.com/singoesdeep/zzrpg/backend/pkg/httpx"
)

// Kernel wires the engine primitives together and runs plugins.
type Kernel struct {
	cfg     *config.Config
	log     *slog.Logger
	reg     *registry.Registry
	bus     bus.EventBus
	mux     *http.ServeMux
	plugins []plugin.Plugin
}

// New builds a kernel with a fresh registry, in-proc event bus, and HTTP mux.
func New(cfg *config.Config, log *slog.Logger) *Kernel {
	return &Kernel{
		cfg: cfg,
		log: log,
		reg: registry.New(),
		bus: bus.NewInProc(log),
		mux: http.NewServeMux(),
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

	ic := &engineContext{ctx: ctx, k: k}
	for _, p := range ordered {
		if err := p.Init(ic); err != nil {
			return fmt.Errorf("plugin %q init: %w", p.Meta().Name, err)
		}
		k.log.Info("plugin initialised", "plugin", p.Meta().Name)
	}

	for _, p := range ordered {
		if err := p.Start(ic); err != nil {
			return fmt.Errorf("plugin %q start: %w", p.Meta().Name, err)
		}
	}

	// Wrap the router with panic recovery (outermost) and request logging,
	// matching the previous main() setup.
	handler := httpx.Recover(k.log)(httpx.RequestLogger(k.log)(k.mux))
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
// delegating to the kernel.
type engineContext struct {
	ctx context.Context
	k   *Kernel
}

func (e *engineContext) Context() context.Context     { return e.ctx }
func (e *engineContext) Logger() *slog.Logger         { return e.k.log }
func (e *engineContext) Config() *config.Config       { return e.k.cfg }
func (e *engineContext) Registry() *registry.Registry { return e.k.reg }
func (e *engineContext) Bus() bus.EventBus            { return e.k.bus }
func (e *engineContext) Mux() *http.ServeMux          { return e.k.mux }

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
