// Package quest is gamekit's objective-tracking toolkit: a quest is a
// sequence of Steps, each an opaque (Type, Target, Count) objective — "KILL_MOB
// wolf x5" to an RPG, "BUILD_UNIT tank x3" to an RTS, "REACH_POP city x1000" to
// a city-builder. Advance is the pure state transition a progress event applies
// to a quest in progress: does it match the current step, how much did it move,
// did the step or the whole quest complete. It has no persistence, no rewards,
// and no character concept — a game owns those and calls Advance for the
// mechanism.
package quest

// Step is one objective within a quest.
type Step struct {
	Type   string
	Target string
	Count  int32
}

// Advance applies one progress event (actionType, target, amount) to a quest at
// currentStep with the given per-step progress. If the event doesn't match the
// current step's (Type, Target), or the step is already at its Count, matched
// is false and progress/currentStep are returned unchanged (progress is never
// mutated in place). Otherwise it returns the updated progress, the step index
// to continue from (advanced past a just-completed non-final step), whether
// this step just completed, and whether the quest (a completed final step) did.
func Advance(steps []Step, currentStep int32, progress []int32, actionType, target string, amount int32) (newProgress []int32, newStep int32, stepCompleted, questCompleted, matched bool) {
	if currentStep < 0 || int(currentStep) >= len(steps) || int(currentStep) >= len(progress) {
		return progress, currentStep, false, false, false
	}
	step := steps[currentStep]
	if step.Type != actionType || step.Target != target {
		return progress, currentStep, false, false, false
	}
	if progress[currentStep] >= step.Count {
		return progress, currentStep, false, false, false
	}

	out := append([]int32(nil), progress...)
	next := out[currentStep] + amount
	if next > step.Count {
		next = step.Count
	}
	out[currentStep] = next

	done := next >= step.Count
	isLast := int(currentStep) == len(steps)-1
	nextStep := currentStep
	if done && !isLast {
		nextStep = currentStep + 1
	}
	return out, nextStep, done, done && isLast, true
}
