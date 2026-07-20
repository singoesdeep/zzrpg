package xpboost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/singoesdeep/zzrpg/backend/engine/hooks"
	"github.com/singoesdeep/zzrpg/backend/engine/plugin/plugintest"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/combat"
)

// TestXPBoostPluginExtensions drives the plugin's Init through the plugintest
// harness and verifies each extension it registers.
func TestXPBoostPluginExtensions(t *testing.T) {
	h := plugintest.New()
	if err := h.Init(&Plugin{ProtectedID: 42}); err != nil {
		t.Fatalf("plugin Init: %v", err)
	}

	// Filter: gold is doubled.
	r := hooks.ApplyFilters(h.Hooks(), context.Background(), character.HookRewards,
		character.RewardsFilter{CharacterID: 1, Gold: 100, Exp: 50})
	if r.Gold != 200 {
		t.Errorf("expected the reward filter to double gold to 200, got %d", r.Gold)
	}
	if r.Exp != 50 {
		t.Errorf("expected exp unchanged, got %d", r.Exp)
	}

	// Veto: attacking the protected target is blocked; others are allowed.
	if err := hooks.DoAction(h.Hooks(), context.Background(), combat.HookPreAttack,
		combat.PreAttack{AttackerID: 1, DefenderID: 42}); err == nil {
		t.Error("expected the pre-attack veto to block an attack on the protected target")
	}
	if err := hooks.DoAction(h.Hooks(), context.Background(), combat.HookPreAttack,
		combat.PreAttack{AttackerID: 1, DefenderID: 9999}); err != nil {
		t.Errorf("expected an attack on a non-protected target to be allowed, got %v", err)
	}

	// Route: the status endpoint is registered.
	rec := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/example/xpboost", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 from the plugin route, got %d", rec.Code)
	}
}
