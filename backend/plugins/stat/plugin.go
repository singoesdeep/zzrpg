// Package stat is the optional stat-engine plugin: it loads the embedded Rust
// zzstat library and provides the "stat" service (damage/derived-stat math).
// Extracting it from the core plugin makes zzstat optional — a game that does
// not need stat math simply omits this plugin and boots without the .so.
package stat

import (
	"context"
	"fmt"

	"github.com/singoesdeep/zzrpg/backend/engine/admin"
	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/platform/statclient"
)

type Plugin struct {
	plugin.Base
	holder *statclient.StatHolder
}

func (*Plugin) AdminInfo() admin.Info {
	return admin.Info{
		Title:       "Stat Engine",
		Description: "Embedded Rust zzstat FFI for derived-stat and damage math",
		Icon:        "fa-calculator",
		Category:    "Infrastructure",
	}
}

func (*Plugin) Meta() plugin.Meta { return plugin.Meta{Name: "stat"} }

func (p *Plugin) Init(ic plugin.InitContext) error {
	client, err := statclient.NewClient(ic.Config().ZzstatGRPCURL)
	if err != nil {
		return fmt.Errorf("load embedded Rust zzstat library: %w", err)
	}
	ic.Logger().Info("Successfully initialized embedded statclient loading Rust zzstat shared library")
	p.holder = &statclient.StatHolder{Client: client}
	return registry.Provide(ic.Registry(), "stat", p.holder)
}

func (p *Plugin) Stop(context.Context) error {
	if p.holder != nil && p.holder.Client != nil {
		return p.holder.Client.Close()
	}
	return nil
}
