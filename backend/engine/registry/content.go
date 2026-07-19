package registry

import "fmt"

// ContentType describes a kind of data-driven content a plugin can register,
// such as "loot_table", "class", or "mob".
//
// Register is invoked by content-loading code (outside this package) to
// parse raw bytes for a given content id into a concrete instance and store
// it via Registry.StoreContent.
type ContentType struct {
	// Kind is the unique name of this content type, e.g. "loot_table".
	Kind string

	// Register parses raw into a content instance identified by id and
	// stores it, typically by calling Registry.StoreContent.
	Register func(id string, raw []byte) error
}

// DefineContentType registers ct.Kind in r.
//
// It returns an error if a content type with the same Kind is already
// defined; the error message names the conflicting kind.
func (r *Registry) DefineContentType(ct ContentType) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.contentTypes[ct.Kind]; exists {
		return fmt.Errorf("registry: content type %q is already defined", ct.Kind)
	}
	r.contentTypes[ct.Kind] = ct
	return nil
}

// Lookup returns the content instance previously stored under kind and id
// via StoreContent. The second return value reports whether it was found.
func (r *Registry) Lookup(kind, id string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	byID, ok := r.contentValues[kind]
	if !ok {
		return nil, false
	}
	v, ok := byID[id]
	return v, ok
}

// StoreContent stores v as the content instance identified by kind and id,
// making it retrievable via Lookup.
//
// It is typically called from within a ContentType.Register implementation
// once raw bytes have been parsed into v. StoreContent does not require the
// kind to have been defined via DefineContentType first; it always succeeds
// unless r is nil.
func (r *Registry) StoreContent(kind, id string, v any) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	byID, ok := r.contentValues[kind]
	if !ok {
		byID = make(map[string]any)
		r.contentValues[kind] = byID
	}
	byID[id] = v
	return nil
}
