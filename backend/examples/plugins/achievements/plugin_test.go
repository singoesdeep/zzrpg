package achievements

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/singoesdeep/zzrpg/backend/engine/plugin/plugintest"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/combat"
)

// waitUnlocked polls until charID has unlocked id, or times out (event handlers
// run asynchronously on the bus).
func waitUnlocked(tr *Tracker, charID int64, id string) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, a := range tr.Unlocked(charID) {
			if a.ID == id {
				return true
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func TestAchievementsPlugin(t *testing.T) {
	h := plugintest.New()
	p := &Plugin{}
	if err := h.Init(p); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// A kill unlocks "first_blood" (threshold 1).
	_ = h.Bus().Publish(context.Background(), combat.MobKilled{KillerID: 7, VictimID: 9999})
	if !waitUnlocked(p.tracker, 7, "first_blood") {
		t.Fatal("expected first_blood to unlock after a kill")
	}

	// Reaching level 10 unlocks "level_10".
	_ = h.Bus().Publish(context.Background(), character.CharacterLeveledUp{CharacterID: 7, NewLevel: 10})
	if !waitUnlocked(p.tracker, 7, "level_10") {
		t.Fatal("expected level_10 to unlock at level 10")
	}

	// The tracker is exposed as a resolvable service.
	if _, err := registry.Resolve[*Tracker](h.Registry(), "achievements"); err != nil {
		t.Errorf("expected the tracker provided as a service, got %v", err)
	}

	// The read endpoint returns the unlocked achievements.
	rec := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/achievements/7", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "first_blood") {
		t.Errorf("endpoint body missing first_blood: %s", rec.Body.String())
	}
}
