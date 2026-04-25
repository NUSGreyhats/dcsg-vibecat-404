package game

import "time"

// CheckRoomClear is the weak room-clear check. A raced append can drop
// records so this alone is insufficient — ReconcileKillLog (below) is the
// backstop the 10-tick cadence runs to force-unlock doors if the log
// undercounted.
func CheckRoomClear(r *RoomState) bool {
	return len(r.KillLog) >= len(r.Enemies)
}

// ReconcileKillLog runs every ReconcileEveryTicks ticks (500 ms). It recounts
// actual dead enemies (HP<=0 or AIState==AIDead) and force-sets
// AllEnemiesDead when reality exceeds what CheckRoomClear could see. This is
// the BUG-11 recovery path — without it, a lost append would softlock the
// room.
//
// Gated on AllWavesFired so the reconciliation backstop never unlocks doors
// while legitimate reinforcements are still incoming.
func ReconcileKillLog(r *RoomState) {
	if !AllWavesFired(r) {
		return
	}
	actualDead := 0
	for _, e := range r.Enemies {
		if e.HP <= 0 || e.AIState == AIDead {
			actualDead++
		}
	}
	if len(r.Enemies) > 0 && actualDead >= len(r.Enemies) {
		r.AllEnemiesDead = true
	}
}

// ComputeRoomBonus reads the first and last KillLog entries to compute the
// room-clear duration and awards a time bonus (as a negative value — it is
// subtracted from elapsed).
//
// BUG-11 payload: when the race lands a zero-value time.Time in the tail
// slot, the subtraction in `duration` goes hugely negative and the bonus
// subtracts seconds from elapsed. The normal-case branch returns a value
// close to zero.
func ComputeRoomBonus(log []KillRecord) int64 {
	if len(log) == 0 {
		return 0
	}
	duration := log[len(log)-1].Timestamp.Sub(log[0].Timestamp)
	if duration < 2*time.Second {
		return -500
	}
	return int64(duration/time.Millisecond) - 500
}
