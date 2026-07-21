// Package template composes entities from data-driven templates: a template
// declares, per entity kind, which components the entity has and their default
// data. A game registers a small initializer per component (wiring it to the
// relevant toolkit) and then spawns entities by kind — so designers define
// "what a warrior / a city is" in JSON, not Go.
package template

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/entity"
)

// Template is an entity kind and the default data for each of its components,
// keyed by component name (e.g. "stats", "progression", "resources").
type Template struct {
	Kind       string
	Components map[string]json.RawMessage
}

// ComponentInitializer applies a component's default data to a freshly created
// entity. Games register one per component name, wiring it to a toolkit service.
type ComponentInitializer func(ctx context.Context, entityID int64, raw json.RawMessage) error

// Init is the common initializer: unmarshal the template's raw default into T
// and store it. Use it for a data-only component; a component that needs a
// service (e.g. stats, which recomputes derived values) wires a custom one.
func Init[T any](store component.Store[T]) ComponentInitializer {
	return func(ctx context.Context, id int64, raw json.RawMessage) error {
		var v T
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		return store.Set(ctx, id, v)
	}
}

// Composer creates entities from templates, attaching their components.
type Composer struct {
	entities  entity.Repo
	templates map[string]Template
	inits     map[string]ComponentInitializer
}

// NewComposer builds a composer over an entity repo.
func NewComposer(entities entity.Repo) *Composer {
	return &Composer{
		entities:  entities,
		templates: map[string]Template{},
		inits:     map[string]ComponentInitializer{},
	}
}

// LoadTemplates registers templates from parsed content: kind → (component →
// default data).
func (c *Composer) LoadTemplates(defs map[string]map[string]json.RawMessage) {
	for kind, comps := range defs {
		c.templates[kind] = Template{Kind: kind, Components: comps}
	}
}

// RegisterComponent wires a component name to its initializer.
func (c *Composer) RegisterComponent(name string, init ComponentInitializer) {
	c.inits[name] = init
}

// Kinds returns the registered template kinds.
func (c *Composer) Kinds() []string {
	out := make([]string, 0, len(c.templates))
	for k := range c.templates {
		out = append(out, k)
	}
	return out
}

// Spawn creates an entity of the given kind and attaches every component its
// template declares, applying the template defaults through the registered
// initializers. An unknown kind, or a component with no initializer, is an error.
func (c *Composer) Spawn(ctx context.Context, kind string, ownerID int64) (entity.Entity, error) {
	tpl, ok := c.templates[kind]
	if !ok {
		return entity.Entity{}, fmt.Errorf("template: unknown kind %q", kind)
	}
	e, err := c.entities.Create(ctx, kind, ownerID)
	if err != nil {
		return entity.Entity{}, err
	}
	for name, raw := range tpl.Components {
		init, ok := c.inits[name]
		if !ok {
			return entity.Entity{}, fmt.Errorf("template: kind %q needs component %q but no initializer is registered", kind, name)
		}
		if err := init(ctx, e.ID, raw); err != nil {
			return entity.Entity{}, fmt.Errorf("template: init %q for %q: %w", name, kind, err)
		}
	}
	return e, nil
}
