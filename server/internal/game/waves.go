package game

import (
	"math"
	"math/rand"
	"time"
)

// seedWaves consumes MobRNG for every declared wave in the order mandated by
// docs/specs/foundations/07-determinism-seeding.md: per wave, one Intn call
// for the arm-delay jitter, then the full 7-step block per mob in declaration
// order. Waves whose threshold rounds to < 1 living are skipped and consume
// no RNG.
//
// Called from LoadRoom after all initial enemies have been seeded. Must run
// in wave-slot order — any reorder invalidates every seed ever analyzed by
// a tool author (invariant in 07-determinism-seeding.md).
func seedWaves(r *RoomState, rng *rand.Rand, waves []WaveDef) {
	for slot, wave := range waves {
		if slot >= len(WaveTriggers) {
			// Room declares more waves than the global trigger table has
			// slots for. Skip silently — authoring error, but do not
			// consume RNG for slots beyond the table or we'd break seed
			// determinism for any room that accidentally over-declares.
			break
		}
		trigger := int(math.Floor(float64(r.InitialPop) * float64(WaveTriggers[slot])))
		if trigger < 1 {
			// Threshold would only arm on full clear — skip, consume no RNG.
			continue
		}

		jitter := rng.Intn(2*WaveArmJitterMs) - WaveArmJitterMs

		mobs := make([]PrecomputedMob, len(wave.Mobs))
		for i, m := range wave.Mobs {
			mobs[i] = PrecomputedMob{
				Type: m.Type,
				Seed: SeedMob(rng, r.RoomIndex, m.X, m.Y),
			}
		}

		r.PendingWaves = append(r.PendingWaves, PendingWave{
			TriggerRemaining: trigger,
			ArmDelayJitter:   jitter,
			Mobs:             mobs,
		})
	}
}

// livingCount returns the number of enemies that are neither dead nor at
// zero/negative HP. Only already-spawned enemies count — pending (unfired)
// wave mobs do NOT count, otherwise the threshold math is wrong.
func livingCount(r *RoomState) int {
	n := 0
	for _, e := range r.Enemies {
		if e.AIState != AIDead && e.HP > 0 {
			n++
		}
	}
	return n
}

// AdvanceWaves runs one tick of the wave state machine. For each pending
// wave: arm when livingCount drops to ≤ TriggerRemaining (one-way latch),
// fire when `now` reaches FireAt. Returns the entity IDs of any mobs that
// were instantiated this tick, for the caller to broadcast.
//
// Consumes no RNG — all randomness was baked in at room load.
func AdvanceWaves(r *RoomState, now time.Time) []uint32 {
	var spawned []uint32

	// Arm pass — compute living once; a wave fire in this same call does
	// not count toward its own threshold (we fire after all arm checks).
	living := livingCount(r)
	for i := range r.PendingWaves {
		w := &r.PendingWaves[i]
		if w.Armed || w.Fired {
			continue
		}
		if living <= w.TriggerRemaining {
			w.Armed = true
			w.FireAt = now.Add(time.Duration(WaveArmDelayMs+w.ArmDelayJitter) * time.Millisecond)
		}
	}

	// Fire pass — one-way latch: armed waves always fire, even if the room
	// cleared during the arm delay. The player armed it by killing fast;
	// they collect the consequence.
	for i := range r.PendingWaves {
		w := &r.PendingWaves[i]
		if !w.Armed || w.Fired {
			continue
		}
		if now.Before(w.FireAt) {
			continue
		}
		for _, pm := range w.Mobs {
			id := r.NextEnemyID
			r.NextEnemyID++
			r.Enemies[id] = InstantiateEnemy(r.RoomIndex, id, pm.Type, pm.Seed, now)
			spawned = append(spawned, id)
		}
		w.Fired = true
	}

	return spawned
}

// AllWavesFired returns true when every PendingWave has Fired == true.
// Used as a gate alongside enemy-death checks — doors must not unlock
// while unfired reinforcements are still incoming.
func AllWavesFired(r *RoomState) bool {
	for _, w := range r.PendingWaves {
		if !w.Fired {
			return false
		}
	}
	return true
}
