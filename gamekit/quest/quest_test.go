package quest

import "testing"

func TestAdvanceIgnoresMismatchedEvent(t *testing.T) {
	steps := []Step{{Type: "KILL_MOB", Target: "wolf", Count: 3}}
	progress := []int32{0}

	_, step, done, qDone, matched := Advance(steps, 0, progress, "KILL_MOB", "boar", 1)
	if matched || done || qDone || step != 0 {
		t.Fatalf("mismatched target should not match: matched=%v done=%v qDone=%v step=%d", matched, done, qDone, step)
	}
}

func TestAdvancePartialProgress(t *testing.T) {
	steps := []Step{{Type: "KILL_MOB", Target: "wolf", Count: 3}}
	progress := []int32{0}

	newProgress, step, done, qDone, matched := Advance(steps, 0, progress, "KILL_MOB", "wolf", 2)
	if !matched || done || qDone || step != 0 {
		t.Fatalf("partial progress wrong: matched=%v done=%v qDone=%v step=%d", matched, done, qDone, step)
	}
	if newProgress[0] != 2 {
		t.Fatalf("progress = %d, want 2", newProgress[0])
	}
	// original slice must not be mutated.
	if progress[0] != 0 {
		t.Fatal("Advance mutated the caller's progress slice")
	}
}

func TestAdvanceStepCompletesAndMovesOn(t *testing.T) {
	steps := []Step{
		{Type: "KILL_MOB", Target: "wolf", Count: 3},
		{Type: "TALK_NPC", Target: "elder", Count: 1},
	}
	progress := []int32{2, 0}

	newProgress, step, done, qDone, matched := Advance(steps, 0, progress, "KILL_MOB", "wolf", 5) // overshoot clamps
	if !matched || !done || qDone {
		t.Fatalf("step completion wrong: matched=%v done=%v qDone=%v", matched, done, qDone)
	}
	if step != 1 {
		t.Fatalf("step = %d, want advance to 1", step)
	}
	if newProgress[0] != 3 { // clamped to Count, not 5
		t.Fatalf("progress[0] = %d, want clamped 3", newProgress[0])
	}
}

func TestAdvanceFinalStepCompletesQuest(t *testing.T) {
	steps := []Step{{Type: "TALK_NPC", Target: "elder", Count: 1}}
	progress := []int32{0}

	_, step, done, qDone, matched := Advance(steps, 0, progress, "TALK_NPC", "elder", 1)
	if !matched || !done || !qDone || step != 0 {
		t.Fatalf("final step should complete quest: matched=%v done=%v qDone=%v step=%d", matched, done, qDone, step)
	}
}

func TestAdvanceNoOpOnAlreadyCompletedStepOrOutOfBounds(t *testing.T) {
	steps := []Step{{Type: "KILL_MOB", Target: "wolf", Count: 3}}

	if _, _, _, _, matched := Advance(steps, 0, []int32{3}, "KILL_MOB", "wolf", 1); matched {
		t.Fatal("already-complete step should not match")
	}
	if _, _, _, _, matched := Advance(steps, 5, []int32{0}, "KILL_MOB", "wolf", 1); matched {
		t.Fatal("out-of-bounds step should not match")
	}
}
