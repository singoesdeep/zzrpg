package combat

import (
	"context"
	"errors"
	"strconv"

	"github.com/singoesdeep/zzrpg/backend/content"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/creature"
)

type mobCreatureResolver struct{ mobs *content.Mobs }

func (r mobCreatureResolver) Resolve(_ context.Context, id int64) (creature.Creature, bool, error) {
	def, ok := r.mobs.Mobs[strconv.FormatInt(id, 10)]
	if !ok {
		return creature.Creature{}, false, nil
	}
	return creature.Creature{
		ID:          id,
		Kind:        creature.KindMob,
		Level:       def.Level,
		Defense:     def.Defense,
		Dex:         def.Dex,
		MaxHP:       def.MaxHP,
		MaxMP:       def.MaxMP,
		LootTableID: def.LootTableID,
		QuestTag:    def.QuestTag,
	}, true, nil
}

type charCreatureResolver struct {
	chars character.CharacterService
	pvp   content.PvPDef
}

func (r charCreatureResolver) Resolve(ctx context.Context, id int64) (creature.Creature, bool, error) {
	c, err := r.chars.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, character.ErrCharacterNotFound) {
			return creature.Creature{}, false, nil
		}
		return creature.Creature{}, false, err
	}
	return creature.Creature{
		ID:          id,
		Kind:        creature.KindCharacter,
		Class:       c.ClassName,
		Level:       c.Level,
		Attack:      c.Stats.DerivedStats["ATTACK"],
		Defense:     c.Stats.DerivedStats["DEFENSE"],
		Dex:         c.Stats.BaseStats["DEX"],
		CritRate:    c.Stats.DerivedStats["CRIT_RATE"],
		MaxHP:       c.Stats.DerivedStats["HP"],
		MaxMP:       c.Stats.DerivedStats["MP"],
		LootTableID: r.pvp.LootTableID,
		QuestTag:    r.pvp.QuestTag,
	}, true, nil
}

type compositeCreatureResolver []creature.Resolver

func (rs compositeCreatureResolver) Resolve(ctx context.Context, id int64) (creature.Creature, bool, error) {
	for _, r := range rs {
		c, ok, err := r.Resolve(ctx, id)
		if err != nil || ok {
			return c, ok, err
		}
	}
	return creature.Creature{}, false, nil
}
