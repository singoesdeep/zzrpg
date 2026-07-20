package combat

import (
	"encoding/json"

	"github.com/singoesdeep/zzrpg/backend/content"
	"github.com/singoesdeep/zzrpg/backend/engine/admin"
	"github.com/singoesdeep/zzrpg/backend/engine/outbox"
	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/game/auth"
	"github.com/singoesdeep/zzrpg/backend/game/character"
	"github.com/singoesdeep/zzrpg/backend/game/combat"
	"github.com/singoesdeep/zzrpg/backend/game/inventory"
	"github.com/singoesdeep/zzrpg/backend/game/killreward"
	"github.com/singoesdeep/zzrpg/backend/game/loot"
	"github.com/singoesdeep/zzrpg/backend/game/quests"
	"github.com/singoesdeep/zzrpg/backend/game/skills"
	"github.com/singoesdeep/zzrpg/backend/platform/session"
	"github.com/singoesdeep/zzrpg/backend/platform/socket"
	"github.com/singoesdeep/zzrpg/backend/platform/statclient"
)

type skillResolver struct{ svc *skills.Service }

func (a skillResolver) Resolve(id string) (combat.SkillEffect, bool) {
	d, ok := a.svc.Resolve(id)
	if !ok {
		return combat.SkillEffect{}, false
	}
	return combat.SkillEffect{
		Multiplier: d.Multiplier,
		FlatDamage: d.FlatDamage,
		ManaCost:   d.ManaCost,
		ClassReq:   d.Class,
	}, true
}

type Plugin struct{ plugin.Base }

func (Plugin) AdminInfo() admin.Info {
	return admin.Info{
		Title:       "Combat Engine",
		Description: "Damage resolution via Rust zzstat FFI, mob/player targeting, and skill execution",
		Icon:        "fa-khanda",
		Category:    "Combat",
		Endpoints:   []string{"GET /api/v1/skills", "WS COMBAT_ATTACK -> COMBAT_DAMAGE"},
	}
}

func (Plugin) Meta() plugin.Meta {
	return plugin.Meta{Name: "combat", Requires: []string{"core", "stat", "character", "inventory", "loot", "quests"}}
}

func (Plugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	// Own this domain's event decoders (moved out of core).
	if decoders, err := registry.Resolve[*outbox.Registry](reg, "eventDecoders"); err == nil {
		combat.RegisterEventDecoders(decoders)
	}

	charService := registry.MustResolve[character.CharacterService](reg, "character")
	invService := registry.MustResolve[inventory.InventoryService](reg, "inventory")
	lootService := registry.MustResolve[loot.LootService](reg, "loot")
	questService := registry.MustResolve[quests.QuestService](reg, "quests")
	stat := registry.MustResolve[*statclient.StatHolder](reg, "stat")
	hub := registry.MustResolve[*socket.Hub](reg, "hub")
	router := registry.MustResolve[*socket.MessageRouter](reg, "msgRouter")
	sessionReg := registry.MustResolve[*session.Registry](reg, "session")

	skillService := skills.NewService()
	ic.Mux().Handle("GET /api/v1/skills", auth.AuthMiddleware(ic.Config().JWTSecret)(skills.ListHandler(skillService)))

	mobs := content.MustLoadMobs()
	creatures := compositeCreatureResolver{
		mobCreatureResolver{mobs},
		charCreatureResolver{charService, mobs.PvP},
	}

	rewarder := killreward.New(creatures, charService, questService, lootService, invService, ic.Bus())
	combatService := combat.NewCombatService(creatures, stat.Client, sessionReg, rewarder, ic.Bus(), ic.Hooks(), skillResolver{skillService})

	router.HandleOwned("COMBAT_ATTACK", "combat", func(client *socket.Client, msg socket.WSMessage) {
		if client.CharacterID == 0 {
			errAck, _ := json.Marshal(map[string]interface{}{
				"type": "COMBAT_ERROR",
				"payload": map[string]interface{}{
					"message": "no character selected",
				},
			})
			client.Send <- errAck
			return
		}

		var payload combat.AttackRequest
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			return
		}
		payload.AttackerID = client.CharacterID

		res, err := combatService.ExecuteAttack(client.Context(), payload)
		if err != nil {
			errAck, _ := json.Marshal(map[string]interface{}{
				"type": "COMBAT_ERROR",
				"payload": map[string]interface{}{
					"message": err.Error(),
				},
			})
			client.Send <- errAck
			return
		}

		broadMsg, _ := json.Marshal(map[string]interface{}{
			"type":    "COMBAT_DAMAGE",
			"payload": res,
		})
		hub.Broadcast <- broadMsg
	})

	return nil
}
