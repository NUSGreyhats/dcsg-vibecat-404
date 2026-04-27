package game

import (
	"encoding/json"
	"math/rand"
	"testing"
	"time"

	"github.com/greyhats/defcon-game/server/internal/leaderboard"
	"github.com/greyhats/defcon-game/server/internal/protocol"
)

// newTimeoutTestEngine constructs an Engine with the minimum state stepTick
// needs: a RunRunning RunState anchored at start, an empty room 1, and a
// fresh PlayerState. Channels are buffered large enough that test sends
// never block. The Submit callback panics if invoked — failed runs must
// never reach the leaderboard, and that's the strongest assertion the test
// can make about it.
func newTimeoutTestEngine(t *testing.T, start time.Time) (*Engine, chan []byte, chan protocol.S2CEnvelope) {
	t.Helper()
	in := make(chan []byte, 16)
	out := make(chan protocol.S2CEnvelope, 64)
	submit := func(*leaderboard.CompletedRun) bool {
		t.Fatalf("Submit called for a failed run — leaderboard contract violated")
		return false
	}
	eng := NewEngine(in, out, submit, nil)
	eng.PlayerID = "test-player"
	eng.DisplayName = "test-player"
	eng.run = &RunState{
		Status:       RunRunning,
		PlayerID:     eng.PlayerID,
		DisplayName:  eng.DisplayName,
		RunStartTime: start,
		Seed:         start.UnixMilli(),
		MobRNG:       rand.New(rand.NewSource(start.UnixMilli())),
		CurrentRoom:  1,
		Splits:       []protocol.SplitData{},
		InputLog:     []InputRecord{},
	}
	eng.player = NewPlayerState()
	eng.room = NewRoomState(1)
	eng.lastInputAt = start
	return eng, in, out
}

// drainOut returns the first S2C_RUN_COMPLETE envelope it finds, or nil if
// none was emitted before the channel went idle.
func drainForRunComplete(out chan protocol.S2CEnvelope) *protocol.RunCompleteData {
	for {
		select {
		case env := <-out:
			if env.Type == protocol.S2CRunComplete {
				if d, ok := env.Data.(protocol.RunCompleteData); ok {
					return &d
				}
			}
		default:
			return nil
		}
	}
}

func TestRunHardCapForceEndsAsFailed(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	eng, _, out := newTimeoutTestEngine(t, start)

	// Step exactly one tick past the cap. drainInput sees an empty queue,
	// the cap check fires, finishRun(RunFailed) runs, stepTick returns.
	eng.stepTick(start.Add(MaxRunDuration + time.Millisecond))

	if eng.run.Status != RunFailed {
		t.Fatalf("expected RunFailed after exceeding MaxRunDuration, got %v", eng.run.Status)
	}
	rc := drainForRunComplete(out)
	if rc == nil {
		t.Fatal("expected S2C_RUN_COMPLETE on out channel, got none")
	}
	if rc.Outcome != protocol.RunOutcomeFailed {
		t.Fatalf("expected outcome=failed, got %q", rc.Outcome)
	}
	if rc.TotalMs < MaxRunDuration.Milliseconds() {
		t.Fatalf("expected TotalMs >= MaxRunDuration (%d), got %d", MaxRunDuration.Milliseconds(), rc.TotalMs)
	}
}

func TestRunIdleTimeoutForceEndsAsFailed(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	eng, _, out := newTimeoutTestEngine(t, start)

	// No input ever arrives; lastInputAt stays at `start`. Step well past
	// IdleTimeout but well within MaxRunDuration so the idle path is the
	// only thing that can trigger.
	eng.stepTick(start.Add(IdleTimeout + time.Millisecond))

	if eng.run.Status != RunFailed {
		t.Fatalf("expected RunFailed after exceeding IdleTimeout, got %v", eng.run.Status)
	}
	rc := drainForRunComplete(out)
	if rc == nil || rc.Outcome != protocol.RunOutcomeFailed {
		t.Fatalf("expected S2C_RUN_COMPLETE outcome=failed, got %+v", rc)
	}
}

func TestRunReceivedInputResetsIdleTimer(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	eng, in, _ := newTimeoutTestEngine(t, start)

	// Tick A: 1s before idle would otherwise fire; queue an InputPacket so
	// drainInput updates lastInputAt to now and the idle check passes.
	tickA := start.Add(IdleTimeout - time.Second)
	enqueueInput(t, in, tickA, 1)
	eng.stepTick(tickA)
	if eng.run.Status != RunRunning {
		t.Fatalf("after tickA the run should still be running; got %v", eng.run.Status)
	}
	if !eng.lastInputAt.Equal(tickA) {
		t.Fatalf("drainInput should have set lastInputAt to %v; got %v", tickA, eng.lastInputAt)
	}

	// Tick B: now well past tickA + IdleTimeout but no further packets.
	// Idle clock is measured against tickA (the last received frame), so
	// this should trip.
	tickB := tickA.Add(IdleTimeout + time.Millisecond)
	eng.stepTick(tickB)
	if eng.run.Status != RunFailed {
		t.Fatalf("after tickB the run should have failed on idle timeout; got %v", eng.run.Status)
	}
}

func TestReplayModeSkipsLivenessChecks(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	eng, _, _ := newTimeoutTestEngine(t, start)
	eng.ReplayMode = true
	// Replays don't go through the leaderboard — replace the panicking
	// Submit with one that's never reached on the failure path either, but
	// the assertion we care about is that finishRun isn't called at all.
	eng.Submit = nil

	// Step well past both caps. ReplayMode must short-circuit the entire
	// liveness block, leaving Status untouched.
	eng.stepTick(start.Add(MaxRunDuration + time.Hour))

	if eng.run.Status != RunRunning {
		t.Fatalf("ReplayMode must not force-end runs on liveness; got %v", eng.run.Status)
	}
}

// enqueueInput marshals an InputPacket into a C2S envelope (Ts within the
// per-packet validation window) and pushes the JSON bytes onto in.
func enqueueInput(t *testing.T, in chan []byte, now time.Time, seq uint32) {
	t.Helper()
	data, err := json.Marshal(protocol.InputPacket{DX: 0, DY: 0})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	env := protocol.Envelope{
		Type: protocol.C2SInput,
		Seq:  seq,
		Ts:   now.UnixMilli(),
		Data: data,
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	in <- raw
}
