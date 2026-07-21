package idlekit

import eidle "github.com/singoesdeep/zzrpg/sdk/engine/idle"

// These are EXAMPLE activities shipped with the pilot. In the framework model
// they are exactly what a game developer writes as a plugin: an
// engine/idle.Producer registered on the shared idle registry. A richer catalog
// (combat stages, gathering lifeskills, buildings) is added the same way, by
// other plugins registering more producers — none of it lives in the core.

// training turns character power and level into gold + exp: the combat-grind loop.
type training struct{}

func (training) Unlocked(eidle.State) bool { return true }
func (training) Produce(min float64, s eidle.State, _ func() float64) eidle.Output {
	var o eidle.Output
	power, level := s.Get("power"), s.Get("level")
	o.Add("gold", int64((power*0.1+level)*min))
	o.Add("exp", int64((level*2)*min))
	return o
}

// gathering turns time into a stockpile of "ore" resource (spent by crafting),
// scaling gently with level — the lifeskill/gather loop.
type gathering struct{}

func (gathering) Unlocked(eidle.State) bool { return true }
func (gathering) Produce(min float64, s eidle.State, _ func() float64) eidle.Output {
	var o eidle.Output
	o.Add("ore", int64((1+s.Get("level")*0.1)*min))
	return o
}
