package xpboost

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/hooks"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/combat"
	"github.com/singoesdeep/zzrpg/backend/pkg/config"
)

// fakeInitContext is a minimal plugin.InitContext for exercising a plugin's Init
// in isolation. (A helper like this is a good candidate to ship in the SDK.)
type fakeInitContext struct {
	hooks *hooks.Hooks
	bus   bus.EventBus
	mux   *http.ServeMux
	reg   *registry.Registry
}

func (f *fakeInitContext) Context() context.Context     { return context.Background() }
func (f *fakeInitContext) Logger() *slog.Logger         { return slog.Default() }
func (f *fakeInitContext) Config() *config.Config       { return &config.Config{} }
func (f *fakeInitContext) Registry() *registry.Registry { return f.reg }
func (f *fakeInitContext) Bus() bus.EventBus            { return f.bus }
func (f *fakeInitContext) Hooks() *hooks.Hooks          { return f.hooks }
func (f *fakeInitContext) Mux() *http.ServeMux          { return f.mux }

func TestXPBoostPluginExtensions(t *testing.T) {
	ic := &fakeInitContext{
		hooks: hooks.New(nil),
		bus:   bus.NewInProc(nil),
		mux:   http.NewServeMux(),
		reg:   registry.New(),
	}
	p := &Plugin{ProtectedID: 42}
	if err := p.Init(ic); err != nil {
		t.Fatalf("plugin Init: %v", err)
	}

	// Filter: gold is doubled.
	r := hooks.ApplyFilters(ic.hooks, context.Background(), character.HookRewards,
		character.RewardsFilter{CharacterID: 1, Gold: 100, Exp: 50})
	if r.Gold != 200 {
		t.Errorf("expected the reward filter to double gold to 200, got %d", r.Gold)
	}
	if r.Exp != 50 {
		t.Errorf("expected exp unchanged, got %d", r.Exp)
	}

	// Veto: attacking the protected target is blocked; others are allowed.
	if err := hooks.DoAction(ic.hooks, context.Background(), combat.HookPreAttack,
		combat.PreAttack{AttackerID: 1, DefenderID: 42}); err == nil {
		t.Error("expected the pre-attack veto to block an attack on the protected target")
	}
	if err := hooks.DoAction(ic.hooks, context.Background(), combat.HookPreAttack,
		combat.PreAttack{AttackerID: 1, DefenderID: 9999}); err != nil {
		t.Errorf("expected an attack on a non-protected target to be allowed, got %v", err)
	}

	// Route: the status endpoint is registered.
	rec := httptest.NewRecorder()
	ic.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/example/xpboost", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 from the plugin route, got %d", rec.Code)
	}
}
