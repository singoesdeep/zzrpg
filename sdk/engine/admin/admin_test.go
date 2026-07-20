package admin_test

import (
	"testing"

	"github.com/singoesdeep/zzrpg/sdk/engine/admin"
)

func TestStateManager_ToggleAndList(t *testing.T) {
	catalog := []admin.PluginState{
		{Name: "core", Status: admin.StatusActive},
		{Name: "character", Status: admin.StatusActive},
		{Name: "idle", Status: admin.StatusActive},
	}

	sm := admin.NewStateManager(catalog)

	if !sm.IsActive("idle") {
		t.Fatalf("expected idle to be ACTIVE initially")
	}

	// 1. Toggle idle OFF
	status, err := sm.Toggle("idle")
	if err != nil {
		t.Fatalf("unexpected error toggling idle: %v", err)
	}
	if status != admin.StatusDisabled {
		t.Fatalf("expected status DISABLED, got %s", status)
	}
	if sm.IsActive("idle") {
		t.Fatalf("expected idle to be DISABLED")
	}
	if !sm.IsActive("character") {
		t.Fatalf("expected character to remain ACTIVE")
	}

	// 2. Toggle idle back ON
	status, err = sm.Toggle("idle")
	if err != nil {
		t.Fatalf("unexpected error toggling idle back on: %v", err)
	}
	if status != admin.StatusActive {
		t.Fatalf("expected status ACTIVE, got %s", status)
	}
	if !sm.IsActive("idle") {
		t.Fatalf("expected idle to be ACTIVE again")
	}

	// 3. Prevent core from being disabled
	if _, err = sm.Toggle("core"); err == nil {
		t.Fatalf("expected error when trying to disable core plugin")
	}

	// 4. List preserves registration order
	list := sm.List()
	if len(list) != 3 || list[0].Name != "core" || list[1].Name != "character" || list[2].Name != "idle" {
		t.Fatalf("expected stable registration order, got %+v", list)
	}
}
