package game

import (
	"testing"
	"time"
)

func TestComputeRoomBonus_NormalClearDoesNotPenalize(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	log := []KillRecord{
		{EntityID: 1, Timestamp: start},
		{EntityID: 2, Timestamp: start.Add(3 * time.Second)},
	}
	if got := ComputeRoomBonus(log); got != 0 {
		t.Fatalf("ComputeRoomBonus normal clear = %d, want 0", got)
	}
}

func TestComputeRoomBonus_FastClearBonus(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	log := []KillRecord{
		{EntityID: 1, Timestamp: start},
		{EntityID: 2, Timestamp: start.Add(time.Second)},
	}
	if got := ComputeRoomBonus(log); got != -500 {
		t.Fatalf("ComputeRoomBonus fast clear = %d, want -500", got)
	}
}

func TestComputeRoomBonus_NegativeDurationPayloadPreserved(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	log := []KillRecord{
		{EntityID: 1, Timestamp: start},
		{EntityID: 2, Timestamp: start.Add(-2 * time.Second)},
	}
	if got := ComputeRoomBonus(log); got != -2500 {
		t.Fatalf("ComputeRoomBonus negative duration = %d, want -2500", got)
	}
}
