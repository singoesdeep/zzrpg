// Package registry provides a concurrency-safe, typed runtime service and
// content registry for the zzrpg backend engine.
//
// It serves two related purposes:
//
//  1. Service registry: plugins and kernel components can Provide a typed
//     service under a string name, and later Resolve it elsewhere without
//     either side needing a shared import of a concrete type — only the
//     type parameter at the call site.
//  2. Content registry: plugins can DefineContentType to describe a kind of
//     data-driven content (e.g. "loot_table", "class", "mob") along with a
//     Register function that parses raw bytes into content instances, which
//     are then stored and looked up by kind+id.
package registry

import (
	"fmt"
	"sort"
	"sync"
)

// Registry is a concurrency-safe, typed service+content registry.
//
// All state is guarded by a single sync.RWMutex; the zero value is not
// usable, construct one with New.
type Registry struct {
	mu sync.RWMutex

	services map[string]any

	contentTypes  map[string]ContentType
	contentValues map[string]map[string]any
}

// New creates a new, empty Registry ready for use.
func New() *Registry {
	return &Registry{
		services:      make(map[string]any),
		contentTypes:  make(map[string]ContentType),
		contentValues: make(map[string]map[string]any),
	}
}

// Provide registers svc under name in r.
//
// Provide is a generic free function rather than a method because Go methods
// cannot carry their own type parameters.
//
// It returns an error if name is already registered; the error message
// names the conflicting key.
func Provide[T any](r *Registry, name string, svc T) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.services[name]; exists {
		return fmt.Errorf("registry: service %q is already registered", name)
	}
	r.services[name] = svc
	return nil
}

// Resolve looks up name in r and type-asserts the stored value to T.
//
// It returns an error if name is not registered (naming the key), or if the
// stored value cannot be assigned to T (naming the key and both the
// requested and stored types).
func Resolve[T any](r *Registry, name string) (T, error) {
	var zero T

	r.mu.RLock()
	svc, exists := r.services[name]
	r.mu.RUnlock()

	if !exists {
		return zero, fmt.Errorf("registry: no service registered under %q", name)
	}

	typed, ok := svc.(T)
	if !ok {
		return zero, fmt.Errorf("registry: service %q is not assignable to requested type %T (stored type %T)", name, zero, svc)
	}
	return typed, nil
}

// MustResolve is like Resolve but panics if resolution fails.
//
// It is intended for use at kernel boot time, where a missing hard
// dependency is a fatal configuration error rather than something the
// caller can recover from.
func MustResolve[T any](r *Registry, name string) T {
	svc, err := Resolve[T](r, name)
	if err != nil {
		panic(err)
	}
	return svc
}

// Names returns the sorted list of all names currently registered as
// services in r. It is intended for diagnostics and introspection.
func Names(r *Registry) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.services))
	for name := range r.services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
