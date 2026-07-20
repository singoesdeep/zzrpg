package registry

// Key binds a service name to its type, giving compile-time safety over the
// bare string-keyed Provide/Resolve: the name and the type travel together, so a
// plugin exports one Key and consumers can neither typo the name nor mismatch
// the type. It coexists with the string API (a Key just wraps a name).
//
//	// in the character plugin
//	var Service = registry.NewKey[character.CharacterService]("character")
//
//	// in a consumer
//	chars := registry.MustResolveKey(reg, character.Service) // type inferred
type Key[T any] struct{ name string }

// NewKey defines a typed service key. Two keys with the same name but different
// types resolve the same registry slot; keep names unique per service.
func NewKey[T any](name string) Key[T] { return Key[T]{name: name} }

// Name returns the underlying registry name, for interop with the string API.
func (k Key[T]) Name() string { return k.name }

// ProvideKey registers svc under key. Equivalent to Provide(r, key.Name(), svc).
func ProvideKey[T any](r *Registry, key Key[T], svc T) error {
	return Provide(r, key.name, svc)
}

// ResolveKey looks up the service registered under key with its bound type.
func ResolveKey[T any](r *Registry, key Key[T]) (T, error) {
	return Resolve[T](r, key.name)
}

// MustResolveKey is ResolveKey but panics on failure (boot-time hard deps).
func MustResolveKey[T any](r *Registry, key Key[T]) T {
	return MustResolve[T](r, key.name)
}
