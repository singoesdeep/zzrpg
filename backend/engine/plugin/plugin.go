// Package plugin defines the contract every engine plugin implements. A plugin
// bundles a slice of game (or infrastructure) behaviour: it registers services
// and content into the registry, wires HTTP routes, subscribes to events, and
// optionally runs background work. The kernel drives plugins through an ordered
// lifecycle (Init -> Start -> Stop) so that construction, wiring, and teardown
// are declarative rather than hand-written in main().
package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/hooks"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/pkg/config"
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

// AdminInfo describes a plugin's administrative UI presentation.
type AdminInfo struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Icon        string   `json:"icon"`        // FontAwesome icon e.g. "fa-shield-halved"
	Category    string   `json:"category"`    // e.g. "Core", "Gameplay", "Economy", "Security"
	Endpoints   []string `json:"endpoints,omitempty"`
}

// PluginInfo combines a plugin's runtime status and administrative metadata for
// rendering in the Admin Dashboard.
type PluginInfo struct {
	Name     string     `json:"name"`
	Requires []string   `json:"requires"`
	Status   string     `json:"status"`
	Admin    *AdminInfo `json:"admin,omitempty"`
}

// AdminDescribor is an optional interface plugins can implement to expose
// administrative details to the Admin Dashboard.
type AdminDescribor interface {
	AdminInfo() AdminInfo
}

// StateManager tracks and toggles runtime activation/deactivation states of registered plugins.
type StateManager struct {
	mu     sync.RWMutex
	states map[string]*PluginInfo
}

// NewStateManager initializes a StateManager with the given plugin catalog.
func NewStateManager(catalog []PluginInfo) *StateManager {
	sm := &StateManager{
		states: make(map[string]*PluginInfo, len(catalog)),
	}
	for i := range catalog {
		p := catalog[i]
		sm.states[p.Name] = &p
	}
	return sm
}

// List returns a snapshot of all registered plugins and their current status.
func (sm *StateManager) List() []PluginInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	res := make([]PluginInfo, 0, len(sm.states))
	for _, s := range sm.states {
		res = append(res, *s)
	}
	return res
}

// IsActive checks if a plugin is currently enabled/active.
func (sm *StateManager) IsActive(name string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if s, ok := sm.states[name]; ok {
		return s.Status == "ACTIVE"
	}
	return true
}

// Toggle flips a plugin's status between ACTIVE and DISABLED.
func (sm *StateManager) Toggle(name string) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	s, ok := sm.states[name]
	if !ok {
		return "", fmt.Errorf("plugin %q not found", name)
	}
	if name == "core" {
		return "", fmt.Errorf("core plugin cannot be disabled")
	}
	if s.Status == "ACTIVE" {
		s.Status = "DISABLED"
	} else {
		s.Status = "ACTIVE"
	}
	return s.Status, nil
}
