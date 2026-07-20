package character

import (
	"context"
	"encoding/json"

	"github.com/singoesdeep/zzrpg/backend/engine/admin"
	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/eventlog"
	"github.com/singoesdeep/zzrpg/backend/engine/outbox"
	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/engine/store"

	"github.com/singoesdeep/zzrpg/backend/internal/auth"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/database"
	"github.com/singoesdeep/zzrpg/backend/internal/inventory"
	"github.com/singoesdeep/zzrpg/backend/internal/session"
	"github.com/singoesdeep/zzrpg/backend/internal/socket"
	"github.com/singoesdeep/zzrpg/backend/internal/statclient"
)

type Plugin struct {
	charService character.CharacterService
	hub         *socket.Hub
	eventBus    bus.EventBus
	sessionReg  *session.Registry
	store       store.Store
	decoders    *outbox.Registry
}

func (Plugin) AdminInfo() admin.Info {
	return admin.Info{
		Title:       "Character System",
		Description: "Character progression, stat calculations, level ups, and active session tracking",
		Icon:        "fa-user-ninja",
		Category:    "Gameplay",
		Endpoints:   []string{"POST /api/v1/characters", "GET /api/v1/characters", "GET /api/v1/characters/{id}", "GET /api/v1/characters/{id}/stats"},
	}
}

func (Plugin) Meta() plugin.Meta {
	return plugin.Meta{Name: "character", Requires: []string{"core"}}
}

func (p *Plugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	mux := ic.Mux()
	cfg := ic.Config()
	log := ic.Logger()

	db := registry.MustResolve[*database.DB](reg, "db")
	stat := registry.MustResolve[*statclient.StatHolder](reg, "stat")
	p.sessionReg = registry.MustResolve[*session.Registry](reg, "session")
	p.store = db.Store
	p.decoders = registry.MustResolve[*outbox.Registry](reg, "eventDecoders")

	p.eventBus = ic.Bus()
	charRepo := character.NewCharacterRepository(db.Store)
	p.charService = character.NewCharacterService(charRepo, stat.Client, nil, ic.Bus(), ic.Hooks())
	if err := registry.Provide(reg, "character", p.charService); err != nil {
		return err
	}

	p.hub = registry.MustResolve[*socket.Hub](reg, "hub")
	router := registry.MustResolve[*socket.MessageRouter](reg, "msgRouter")
	router.HandleOwned("SELECT_CHARACTER", "character", p.handleSelectCharacter)

	// Character endpoints (protected by JWT).
	mux.Handle("POST /api/v1/characters", auth.AuthMiddleware(cfg.JWTSecret)(character.CreateHandler(p.charService)))
	mux.Handle("GET /api/v1/characters", auth.AuthMiddleware(cfg.JWTSecret)(character.ListHandler(p.charService)))
	mux.Handle("GET /api/v1/characters/{id}", auth.AuthMiddleware(cfg.JWTSecret)(character.GetHandler(p.charService)))
	mux.Handle("GET /api/v1/characters/{id}/stats", auth.AuthMiddleware(cfg.JWTSecret)(character.GetStatsHandler(p.charService)))

	// Stat recalculation on equip/unequip.
	eventBus := ic.Bus()
	eventBus.Subscribe(inventory.EventItemEquipped, func(ctx context.Context, ev bus.Event) {
		e, ok := ev.(inventory.ItemEquipped)
		if !ok {
			return
		}
		log.Info("Item equipped event received, triggering stat recalculation", "character_id", e.CharacterID)
		if err := p.charService.RecalculateStats(ctx, int64(e.CharacterID)); err != nil {
			log.Error("Failed to recalculate stats on equip", "character_id", e.CharacterID, "error", err)
		}
	})
	eventBus.Subscribe(inventory.EventItemUnequipped, func(ctx context.Context, ev bus.Event) {
		e, ok := ev.(inventory.ItemUnequipped)
		if !ok {
			return
		}
		log.Info("Item unequipped event received, triggering stat recalculation", "character_id", e.CharacterID)
		if err := p.charService.RecalculateStats(ctx, int64(e.CharacterID)); err != nil {
			log.Error("Failed to recalculate stats on unequip", "character_id", e.CharacterID, "error", err)
		}
	})

	return nil
}

func (p *Plugin) Start(plugin.RunContext) error { return nil }

func (p *Plugin) Stop(context.Context) error { return nil }

func (p *Plugin) handleSelectCharacter(client *socket.Client, msg socket.WSMessage) {
	var payload socket.SelectCharPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return
	}
	p.hub.AssociateCharacter(client, payload.CharacterID)

	char, err := p.charService.GetByID(client.Context(), payload.CharacterID)
	if err == nil {
		p.sessionReg.StartSession(payload.CharacterID, char.Stats.DerivedStats["HP"], char.Stats.DerivedStats["MP"])

		if p.eventBus != nil {
			_ = p.eventBus.Publish(client.Context(), character.CharacterLoggedIn{
				CharacterID:  payload.CharacterID,
				LastActiveAt: char.LastActiveAt,
			})
		}

		if p.decoders != nil {
			recorded, rerr := eventlog.Replay(client.Context(), p.store, p.decoders,
				eventlog.CharacterStream(payload.CharacterID), char.LastActiveAt)
			if rerr == nil && len(recorded) > 0 {
				events := make([]map[string]interface{}, 0, len(recorded))
				for _, r := range recorded {
					events = append(events, map[string]interface{}{
						"type":        r.Event.Name(),
						"occurred_at": r.OccurredAt,
						"payload":     r.Event,
					})
				}
				awayPkt, _ := json.Marshal(map[string]interface{}{
					"type":    "AWAY_EVENTS",
					"payload": map[string]interface{}{"events": events},
				})
				client.Send <- awayPkt
			}
		}

		_ = p.charService.UpdateLastActive(client.Context(), payload.CharacterID)
	}

	ack, _ := json.Marshal(map[string]interface{}{
		"type": "SELECT_CHARACTER_ACK",
		"payload": map[string]interface{}{
			"character_id": payload.CharacterID,
			"status":       "ACTIVE",
		},
	})
	client.Send <- ack
}
