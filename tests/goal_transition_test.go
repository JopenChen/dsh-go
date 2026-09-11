// Package tests 的 goal 阶段迁移合法性验收测试。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/goal"
)

func TestCanTransitionActiveToPaused(t *testing.T) {
	if !goal.CanTransition(goal.PhaseActive, goal.PhasePaused) {
		t.Fatal("active -> paused should be allowed")
	}
}

func TestCanTransitionPausedToBlockedRejected(t *testing.T) {
	if goal.CanTransition(goal.PhasePaused, goal.PhaseBlocked) {
		t.Fatal("paused -> blocked must be rejected")
	}
}

func TestCanTransitionCompleteTerminal(t *testing.T) {
	if goal.CanTransition(goal.PhaseComplete, goal.PhaseActive) {
		t.Fatal("complete is terminal, no outgoing transition")
	}
}

func TestCanTransitionBlockedToActive(t *testing.T) {
	if !goal.CanTransition(goal.PhaseBlocked, goal.PhaseActive) {
		t.Fatal("blocked -> active (resume) should be allowed")
	}
}

func TestCanTransitionActiveToComplete(t *testing.T) {
	if !goal.CanTransition(goal.PhaseActive, goal.PhaseComplete) {
		t.Fatal("active -> complete should be allowed")
	}
}

func TestCanTransitionInvalidPhase(t *testing.T) {
	if goal.CanTransition(goal.Phase("bogus"), goal.PhaseActive) {
		t.Fatal("invalid source phase must be rejected")
	}
}
