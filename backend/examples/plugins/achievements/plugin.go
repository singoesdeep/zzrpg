// Package achievements is a second example zzrpg plugin, showing a different
// pattern from xpboost: a purely EVENT-DRIVEN, stateful plugin. It uses no hooks
// — it only reacts to domain events, keeps its own state, provides a service
// other plugins can query, and exposes a read endpoint.
//
// It watches kills, quest completions, and level-ups, and unlocks achievements
// when thresholds are crossed. Register it like any plugin:
//
//	k.Register(&achievements.Plugin{})
//
// See docs/PLUGIN_GUIDE.md.
package achievements

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/combat"
	"github.com/singoesdeep/zzrpg/backend/internal/quests"
)

// Achievement is a single unlockable, tracked by a counter of Kind reaching
// Threshold ("kills", "quests") or a level reaching Threshold ("level").
type Achievement struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Threshold int    `json:"threshold"`
}

var defaultAchievements = []Achievement{
	{"first_blood", "First Blood", "kills", 1},
	{"slayer", "Slayer", "kills", 10},
	{"quest_novice", "Quest Novice", "quests", 1},
	{"level_10", "Seasoned", "level", 10},
}

// Tracker holds per-character progress and unlocked achievements. It is safe for
// concurrent use (event handlers run on their own goroutines).
type Tracker struct {
	log  *slog.Logger
	defs []Achievement

	mu       sync.Mutex
	kills    map[int64]int
	quests   map[int64]int
	unlocked map[int64]map[string]bool
}

// NewTracker builds a Tracker with the default achievements.
func NewTracker(log *slog.Logger) *Tracker {
	if log == nil {
		log = slog.Default()
	}
	return &Tracker{
		log:      log,
		defs:     defaultAchievements,
		kills:    map[int64]int{},
		quests:   map[int64]int{},
		unlocked: map[int64]map[string]bool{},
	}
}

func (t *Tracker) recordKill(charID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.kills[charID]++
	t.check(charID, "kills", t.kills[charID])
}

func (t *Tracker) recordQuest(charID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.quests[charID]++
	t.check(charID, "quests", t.quests[charID])
}

func (t *Tracker) recordLevel(charID int64, level int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.check(charID, "level", level)
}

// check unlocks any not-yet-unlocked achievement of kind whose threshold value
// has been reached. Caller holds t.mu.
func (t *Tracker) check(charID int64, kind string, value int) {
	for _, a := range t.defs {
		if a.Kind != kind || value < a.Threshold {
			continue
		}
		if t.unlocked[charID] == nil {
			t.unlocked[charID] = map[string]bool{}
		}
		if !t.unlocked[charID][a.ID] {
			t.unlocked[charID][a.ID] = true
			t.log.Info("achievement unlocked", "character", charID, "achievement", a.ID)
		}
	}
}

// Unlocked returns the achievements a character has earned (a snapshot).
func (t *Tracker) Unlocked(charID int64) []Achievement {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []Achievement
	for _, a := range t.defs {
		if t.unlocked[charID][a.ID] {
			out = append(out, a)
		}
	}
	return out
}

// Plugin is the event-driven achievements plugin.
type Plugin struct {
	plugin.Base
	tracker *Tracker
}

// Meta requires the producers whose events it consumes, so they are present.
func (Plugin) Meta() plugin.Meta {
	return plugin.Meta{Name: "achievements", Requires: []string{"core", "combat", "character", "quests"}}
}

// Init builds the tracker, subscribes it to the relevant events, provides it as a
// service, and registers a read endpoint.
func (p *Plugin) Init(ic plugin.InitContext) error {
	p.tracker = NewTracker(ic.Logger())

	ic.Bus().Subscribe(combat.EventMobKilled, func(_ context.Context, ev bus.Event) {
		if k, ok := ev.(combat.MobKilled); ok {
			p.tracker.recordKill(k.KillerID)
		}
	})
	ic.Bus().Subscribe(quests.EventQuestCompleted, func(_ context.Context, ev bus.Event) {
		if q, ok := ev.(quests.QuestCompleted); ok {
			p.tracker.recordQuest(int64(q.CharacterID))
		}
	})
	ic.Bus().Subscribe(character.EventCharacterLeveledUp, func(_ context.Context, ev bus.Event) {
		if l, ok := ev.(character.CharacterLeveledUp); ok {
			p.tracker.recordLevel(l.CharacterID, int(l.NewLevel))
		}
	})

	// Make the tracker resolvable so other plugins can query achievements.
	if err := registry.Provide(ic.Registry(), "achievements", p.tracker); err != nil {
		return err
	}

	// GET /api/v1/achievements/{id} — a character's unlocked achievements.
	ic.Mux().HandleFunc("GET /api/v1/achievements/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"achievements": p.tracker.Unlocked(id)})
	})

	return nil
}
