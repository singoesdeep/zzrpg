// Package admin holds the administrative-presentation contract for plugins and
// the runtime activation state manager that backs the Admin Dashboard.
//
// It is deliberately separate from engine/plugin: the plugin package defines
// what a plugin *is* (its lifecycle and dependencies), while this package
// defines how a plugin is *presented and toggled* in an operator UI. The engine
// core stays free of presentation concerns, and plugins opt in by implementing
// Describor.
package admin

import (
	"fmt"
	"sync"
)

// Status values a plugin can be in at runtime.
const (
	StatusActive   = "ACTIVE"
	StatusDisabled = "DISABLED"
)

// Info describes a plugin's administrative UI presentation. It is a generic
// presentation descriptor and carries no engine or RPG semantics.
type Info struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Icon        string   `json:"icon"`     // e.g. a FontAwesome class "fa-shield-halved"
	Category    string   `json:"category"` // e.g. "Core", "Gameplay", "Economy", "Security"
	Endpoints   []string `json:"endpoints,omitempty"`
}

// Describor is the optional interface a plugin implements to expose
// administrative metadata to the Admin Dashboard.
type Describor interface {
	AdminInfo() Info
}

// PluginState combines a plugin's runtime status and administrative metadata for
// rendering in the Admin Dashboard.
type PluginState struct {
	Name     string   `json:"name"`
	Requires []string `json:"requires"`
	Status   string   `json:"status"`
	Admin    *Info    `json:"admin,omitempty"`
}

// StateManager is the single source of truth for plugins' runtime activation
// state. It tracks and toggles ACTIVE/DISABLED status and is safe for
// concurrent use.
type StateManager struct {
	mu     sync.RWMutex
	states map[string]*PluginState
	order  []string // preserves registration order for stable listing
}

// NewStateManager initializes a StateManager from the given plugin catalog.
func NewStateManager(catalog []PluginState) *StateManager {
	sm := &StateManager{
		states: make(map[string]*PluginState, len(catalog)),
		order:  make([]string, 0, len(catalog)),
	}
	for _, item := range catalog {
		cp := item
		sm.states[cp.Name] = &cp
		sm.order = append(sm.order, cp.Name)
	}
	return sm
}

// List returns a snapshot of all registered plugins in registration order.
func (sm *StateManager) List() []PluginState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	res := make([]PluginState, 0, len(sm.order))
	for _, name := range sm.order {
		res = append(res, *sm.states[name])
	}
	return res
}

// IsActive reports whether a plugin is currently enabled. Unknown plugins are
// treated as active so callers that gate on a name they did not register do not
// silently disable themselves.
func (sm *StateManager) IsActive(name string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if s, ok := sm.states[name]; ok {
		return s.Status == StatusActive
	}
	return true
}

// Toggle flips a plugin's status between ACTIVE and DISABLED and returns the new
// status. The core plugin cannot be disabled.
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
	if s.Status == StatusActive {
		s.Status = StatusDisabled
	} else {
		s.Status = StatusActive
	}
	return s.Status, nil
}
