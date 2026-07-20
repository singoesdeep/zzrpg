// Package idle is the game-agnostic offline-accrual framework: it turns the
// time a subject spent away into produced output by driving registered
// Producers. It contains zero game concepts — no gold, exp, combat, or
// resources — only the mechanism (elapsed-time window, a producer contract, and
// a registry). Games implement concrete Producers (combat stages, gathering
// lifeskills, RTS resource generators, …) and interpret the opaque Output.
package idle

import "sort"

// State carries the numeric inputs a Producer scales its output by, e.g.
// "power", "skill_level", or "building_level". It is an open map so new
// producers can introduce new inputs without touching the framework.
type State struct {
	Vars map[string]float64
}

// Get returns the named input, or 0 if absent.
func (s State) Get(name string) float64 { return s.Vars[name] }

// Drop is a quantity of a named thing produced over the interval — an item id, a
// resource id, whatever the game defines. The framework never interprets ID.
type Drop struct {
	ID       string
	Quantity int
}

// Output is what a Producer yields over an interval: a ledger of named numeric
// Amounts (e.g. "gold", "exp", "wood") plus discrete Drops. Both are opaque to
// the framework; the game maps them onto its own systems.
type Output struct {
	Amounts map[string]int64
	Drops   []Drop
}

// Add adds n to the named amount, allocating the ledger on first use.
func (o *Output) Add(name string, n int64) {
	if n == 0 {
		return
	}
	if o.Amounts == nil {
		o.Amounts = make(map[string]int64)
	}
	o.Amounts[name] += n
}

// AddDrop appends a drop (ignoring non-positive quantities).
func (o *Output) AddDrop(id string, qty int) {
	if qty <= 0 {
		return
	}
	o.Drops = append(o.Drops, Drop{ID: id, Quantity: qty})
}

// Producer turns elapsed idle minutes and a state into output. Implementations
// must be pure with respect to rng (their only source of randomness) so the
// framework — and tests — can drive them deterministically.
type Producer interface {
	// Unlocked reports whether the subject may run this producer at all.
	Unlocked(s State) bool
	// Produce yields the output accrued over elapsedMin minutes. rng returns
	// values in [0,1).
	Produce(elapsedMin float64, s State, rng func() float64) Output
}

// Window clamps an elapsed duration to the accrual bounds. It returns the
// elapsed minutes and ok=false when less than minSeconds has passed (nothing
// accrues). capSeconds<=0 means uncapped.
func Window(elapsedSeconds, minSeconds, capSeconds float64) (elapsedMin float64, ok bool) {
	if elapsedSeconds < minSeconds {
		return 0, false
	}
	if capSeconds > 0 && elapsedSeconds > capSeconds {
		elapsedSeconds = capSeconds
	}
	return elapsedSeconds / 60.0, true
}

// Registry maps activity ids to Producers. It is populated at startup and read
// concurrently; register before serving.
type Registry struct {
	producers map[string]Producer
	order     []string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{producers: make(map[string]Producer)}
}

// Register adds a producer under id, overwriting any previous one.
func (r *Registry) Register(id string, p Producer) {
	if _, exists := r.producers[id]; !exists {
		r.order = append(r.order, id)
	}
	r.producers[id] = p
}

// Get looks up a producer by id.
func (r *Registry) Get(id string) (Producer, bool) {
	p, ok := r.producers[id]
	return p, ok
}

// IDs returns the registered activity ids in registration order.
func (r *Registry) IDs() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// SortedAmounts returns an Output's ledger keys sorted, for stable iteration.
func SortedAmounts(o Output) []string {
	keys := make([]string, 0, len(o.Amounts))
	for k := range o.Amounts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
