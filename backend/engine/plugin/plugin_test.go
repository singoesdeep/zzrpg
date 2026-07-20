package plugin_test

import (
	"testing"

	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
)

func TestStateManager_ToggleAndList(t *testing.T) {
	catalog := []plugin.PluginInfo{
		{Name: "core", Status: "ACTIVE"},
		{Name: "character", Status: "ACTIVE"},
		{Name: "idle", Status: "ACTIVE"},
	}

	sm := plugin.NewStateManager(catalog)

	if !sm.IsActive("idle") {
		t.Fatalf("expected idle to be ACTIVE initially")
	}

	// 1. Toggle idle OFF
	status, err := sm.Toggle("idle")
	if err != nil {
		t.Fatalf("unexpected error toggling idle: %v", err)
	}
	if status != "DISABLED" {
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
	if status != "ACTIVE" {
		t.Fatalf("expected status ACTIVE, got %s", status)
	}
	if !sm.IsActive("idle") {
		t.Fatalf("expected idle to be ACTIVE again")
	}

	// 3. Prevent core from being disabled
	_, err = sm.Toggle("core")
	if err == nil {
		t.Fatalf("expected error when trying to disable core plugin")
	}
}
