// Package replay drives a stored CompletedRun back through the live
// game.Engine in ReplayMode, fanning out the resulting S2C packets to a
// single connected viewer. Sibling to package spectator: spectator
// observes a live engine; replay reconstructs one from a stored InputLog.
//
// See docs/specs/foundations/08-leaderboard-replay.md#streaming-replay-engine
// and docs/specs/client/11-replay-viewer.md.
package replay

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/greyhats/defcon-game/server/internal/game"
	"github.com/greyhats/defcon-game/server/internal/protocol"
)

// MinSpeed and MaxSpeed clamp the playback speed multiplier. Below
// MinSpeed the playback is indistinguishable from a long pause; above
// MaxSpeed the engine has no headroom to keep the viewer fed without
// dropping frames on slow networks.
const (
	MinSpeed = 0.25
	MaxSpeed = 8.0
)

// OutChBuf sizes the forward channel between the engine's outCh and the
// viewer's WS writer. Same size as the live engine's OutChBuf — drop-on-
// full discipline applies on the WS writer side, not here.
const OutChBuf = 64

// FastForwardSpeed is the (very fast) effective speed used when seeking
// to a target tick during a Restart. Currently restart resets to T0; if
// future revisions add scrubbing, this is the headroom for "rewind to
// T0 then catch up to ms X without sleeping".
const FastForwardSpeed = 1000.0

// PostLogGraceTicks is how long the engine keeps ticking after the
// recorded InputLog is exhausted before the player goroutine
// force-finishes the run. Sized to roughly cover natural termination
// paths the engine can still complete on its own — the 2-tick room
// settle plus the boss death pass plus a small slack. Beyond this
// budget we assume the recording's natural termination won't reproduce
// (BUG-06 settle race, projectile collision races, etc.) and call
// Engine.ForceFinishReplay so the viewer sees S2C_RUN_COMPLETE rather
// than ticking forever.
const PostLogGraceTicks = 40

// commandKind enumerates the verbs the WS reader can dispatch into the
// player goroutine. set_speed carries a Speed value; the others ignore it.
type commandKind uint8

const (
	cmdPlay commandKind = iota
	cmdPause
	cmdSetSpeed
	cmdRestart
)

type command struct {
	Kind  commandKind
	Speed float32
}

// Player owns the lifecycle of one viewer's replay session. Run drives
// the engine; the Pause/Play/SetSpeed/Restart helpers send commands to
// the goroutine. OutCh receives the S2C envelopes the viewer must
// render. Closed by Run on exit.
type Player struct {
	PlayerID    string
	DisplayName string
	Seed        int64
	// TotalMs is the recording's canonical leaderboard total — used as
	// the synthetic total when the engine has to be force-finished
	// because the recorded run's natural termination didn't reproduce.
	TotalMs int64
	Log     []game.InputRecord

	// OutCh is where Run forwards the engine's S2C envelopes. Closed
	// when Run exits; the WS writer should treat the close as "session
	// ended, terminate the viewer".
	OutCh chan protocol.S2CEnvelope

	// cmdCh is the control queue. Buffered so callers don't block on a
	// busy player goroutine.
	cmdCh chan command

	// activeInCh is the engine's InCh for the *current* iteration of
	// runOnce. Held on the struct so advance() can push input records
	// for the upcoming tick. Reset on restart.
	activeInCh chan []byte

	// State owned by the player goroutine — advance() reads/writes,
	// command intake mutates via applyCmd(). No atomics needed because
	// only the goroutine driving advance() touches these fields.
	nextTick    uint64
	logCursor   int
	lastLogTick uint64
	speed       float32
	paused      bool
	restart     bool
	ctx         context.Context
}

// New constructs a Player around a loaded CompletedRun. The caller is
// responsible for unmarshaling the stored input_log blob into
// []game.InputRecord — see leaderboard.GetReplay for the on-disk shape.
// totalMs is the recording's canonical total; used as the fallback
// total when ForceFinishReplay terminates the engine.
func New(playerID, displayName string, seed, totalMs int64, log []game.InputRecord) *Player {
	return &Player{
		PlayerID:    playerID,
		DisplayName: displayName,
		Seed:        seed,
		TotalMs:     totalMs,
		Log:         log,
		OutCh:       make(chan protocol.S2CEnvelope, OutChBuf),
		cmdCh:       make(chan command, 8),
	}
}

// Pause halts playback. Idempotent.
func (p *Player) Pause() { p.enqueue(command{Kind: cmdPause}) }

// Play resumes playback at the current speed. Idempotent.
func (p *Player) Play() { p.enqueue(command{Kind: cmdPlay}) }

// SetSpeed changes the playback multiplier. Values outside [MinSpeed,
// MaxSpeed] are clamped. SetSpeed does NOT implicitly resume playback;
// the caller must Play() if the player is paused.
func (p *Player) SetSpeed(s float32) {
	if s < MinSpeed {
		s = MinSpeed
	}
	if s > MaxSpeed {
		s = MaxSpeed
	}
	p.enqueue(command{Kind: cmdSetSpeed, Speed: s})
}

// Restart resets the engine to T0 and replays from the beginning. The
// viewer sees a fresh S2C_RUN_STARTED followed by the canonical stream.
func (p *Player) Restart() { p.enqueue(command{Kind: cmdRestart}) }

func (p *Player) enqueue(c command) {
	select {
	case p.cmdCh <- c:
	default:
		// Buffer full — caller is mashing buttons faster than the
		// player goroutine can process. Drop is fine; the user-visible
		// state already matched their previous click.
	}
}

// Run drives the replay session until ctx is cancelled or the recording
// terminates. Closes OutCh on exit so the WS writer can detect the end.
func (p *Player) Run(ctx context.Context) {
	defer close(p.OutCh)
	for {
		if !p.runOnce(ctx) {
			return
		}
	}
}

// runOnce performs one full play-through of the recording. Returns true
// to restart (a Restart command was processed), false to exit.
func (p *Player) runOnce(parent context.Context) bool {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	p.ctx = ctx
	p.nextTick = 0
	p.logCursor = 0
	p.speed = 1.0
	p.paused = false
	p.restart = false
	p.lastLogTick = 0
	if n := len(p.Log); n > 0 {
		p.lastLogTick = p.Log[n-1].Tick
	}

	// Drain any commands queued before this run started. Without this, a
	// Restart triggered while pause/set_speed commands were still buffered
	// would carry the stale state into the fresh play-through (e.g. the
	// new run starts paused or at last run's speed). Every play-through
	// starts at 1.0× and unpaused; the user must re-issue intent.
drainCmds:
	for {
		select {
		case <-p.cmdCh:
		default:
			break drainCmds
		}
	}

	// inCh must hold every record for a single tick at once: advance()
	// front-loads all records for nextTick before stepTick gets a chance
	// to drain. Live recording can produce ticks with more than InChBuf
	// records — drainInput pops items in a tight loop while the WS reader
	// concurrently refills, so the recorded burst is bounded by client
	// send-rate, not channel capacity. Sizing inCh to the worst observed
	// burst keeps feedRecord's drop-on-full path from firing on bursty
	// (typically fast) runs and silently desynchronising the replay.
	inCh := make(chan []byte, max(game.InChBuf, maxBurstPerTick(p.Log)+8))
	outCh := make(chan protocol.S2CEnvelope, game.OutChBuf)
	p.activeInCh = inCh

	eng := game.NewEngine(inCh, outCh, nil, nil)
	eng.ReplayMode = true
	eng.PlayerID = p.PlayerID
	eng.DisplayName = p.DisplayName

	// Feed the synthetic handshake before RunReplay invokes handshake().
	// The engine's awaitAuth / awaitStartRun will see these on the next
	// drain and proceed to the tick loop.
	p.feedAuth(inCh)
	p.feedStartRun(inCh)

	// Forward engine envelopes to the viewer. We deliberately do NOT
	// close p.OutCh here — only Run does, on session end. A restart
	// re-uses the same p.OutCh, so the viewer doesn't see a stream
	// interruption between play-throughs.
	forwardDone := make(chan struct{})
	go p.forward(ctx, outCh, forwardDone)

	eng.RunReplay(ctx, p.advance)

	// If the engine didn't terminate naturally (boss kill / RunFailed
	// already reached), the recorded InputLog must have run out without
	// the engine reproducing the run's natural endpoint. Force-finish
	// with the recorded total_ms so the viewer sees S2C_RUN_COMPLETE
	// rather than ticking forever or going dark. ForceFinishReplay is a
	// no-op on terminal runs, so it's safe to always call.
	eng.ForceFinishReplay(p.TotalMs)

	// Engine done. Close the engine's inCh so any pending awaitAuth
	// blocked on it exits, then close outCh so forward drains and
	// returns. Order matters: outCh is the side forward reads from.
	close(inCh)
	close(outCh)
	<-forwardDone

	// Restart was requested — loop back into runOnce with a fresh engine.
	// Cancellation (ctx.Done) takes precedence: even if restart was set
	// just before the parent ctx was cancelled, exit cleanly.
	if p.restart {
		select {
		case <-parent.Done():
			return false
		default:
			return true
		}
	}
	return false
}

// advance is the per-tick callback handed to Engine.RunReplay. It pushes
// the upcoming tick's recorded inputs onto the engine's InCh, then
// sleeps for the inter-tick budget at the current playback speed,
// processing pause/play/speed commands while sleeping. Returns the
// synthetic next `now` and false to terminate the inner loop (cancel /
// restart).
func (p *Player) advance(prev time.Time) (time.Time, bool) {
	p.nextTick++
	for p.logCursor < len(p.Log) && p.Log[p.logCursor].Tick == p.nextTick {
		p.feedRecord(p.activeInCh, p.Log[p.logCursor])
		p.logCursor++
	}
	// Stop ticking once we've run PostLogGraceTicks past the last
	// recorded input. The engine still gets that grace window to reach
	// natural termination (boss kill etc.); past it, runOnce calls
	// Engine.ForceFinishReplay so the viewer terminates cleanly.
	if p.logCursor >= len(p.Log) && p.nextTick > p.lastLogTick+PostLogGraceTicks {
		return time.Time{}, false
	}
	if !p.sleepForSpeed() {
		return time.Time{}, false
	}
	return prev.Add(game.TickDuration), true
}

// sleepForSpeed waits the inter-tick budget at the current speed. If
// paused, blocks indefinitely on the command channel. Returns false on
// ctx.Done() or if a Restart command was processed.
func (p *Player) sleepForSpeed() bool {
	for {
		if p.paused {
			select {
			case <-p.ctx.Done():
				return false
			case c := <-p.cmdCh:
				p.applyCmd(c)
				if p.restart {
					return false
				}
				continue
			}
		}
		d := time.Duration(float64(game.TickDuration) / float64(p.speed))
		// At extreme speeds (FastForwardSpeed) the computed sleep can
		// round to zero; treat that as "yield and continue" so we don't
		// starve other goroutines on a tight loop.
		if d <= 0 {
			d = time.Microsecond
		}
		t := time.NewTimer(d)
		select {
		case <-p.ctx.Done():
			t.Stop()
			return false
		case c := <-p.cmdCh:
			t.Stop()
			p.applyCmd(c)
			if p.restart {
				return false
			}
			continue
		case <-t.C:
			return true
		}
	}
}

func (p *Player) applyCmd(c command) {
	switch c.Kind {
	case cmdPlay:
		p.paused = false
	case cmdPause:
		p.paused = true
	case cmdSetSpeed:
		s := c.Speed
		if s < MinSpeed {
			s = MinSpeed
		}
		if s > MaxSpeed {
			s = MaxSpeed
		}
		p.speed = s
	case cmdRestart:
		p.restart = true
	}
}

// feedAuth pushes a synthetic C2S_AUTH envelope onto the engine's InCh
// to satisfy awaitAuth. Replay mode skips the AuthCheck callback (it's
// nil for the engine we constructed), so the engine accepts the
// PlayerID at face value.
func (p *Player) feedAuth(in chan<- []byte) {
	data, _ := json.Marshal(protocol.AuthPacket{PlayerID: p.PlayerID})
	env := protocol.Envelope{Type: protocol.C2SAuth, Seq: 0, Ts: 0, Data: data}
	raw, err := json.Marshal(env)
	if err != nil {
		log.Printf("replay: marshal auth: %v", err)
		return
	}
	in <- raw
}

// feedStartRun pushes a synthetic C2S_START_RUN with the recorded Seed
// (Seed is the original start_time and double-duties as the RNG seed).
// ReplayMode skips the ±30min SeedWindow check so an old recording is
// accepted regardless of how long ago it was captured.
func (p *Player) feedStartRun(in chan<- []byte) {
	data, _ := json.Marshal(protocol.StartRunPacket{StartTime: p.Seed})
	env := protocol.Envelope{Type: protocol.C2SStartRun, Seq: 0, Ts: 0, Data: data}
	raw, err := json.Marshal(env)
	if err != nil {
		log.Printf("replay: marshal start_run: %v", err)
		return
	}
	in <- raw
}

// feedRecord pushes a single InputRecord back onto the engine's InCh as
// a JSON envelope. The engine's dispatch path then unmarshals as if the
// packet had just arrived from a live WS — minus the Ts ±2s check
// (ReplayMode skips that).
//
// runOnce sizes inCh to fit the worst single-tick burst observed in the
// log, so the non-blocking send below should never hit the default
// branch. The branch remains as defence-in-depth: a dropped record can
// cascade into severe divergence (e.g. losing C2S_ENTER_DOOR keeps the
// viewer in the previous room while subsequent inputs target the new
// one), so the log line is the signal that something pathological got
// past the sizing logic.
func (p *Player) feedRecord(in chan<- []byte, rec game.InputRecord) {
	env := protocol.Envelope{
		Type: rec.Type,
		Seq:  rec.Seq,
		Ts:   rec.Ts,
		Data: rec.Data,
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return
	}
	select {
	case in <- raw:
	default:
		log.Printf("replay: inCh full at tick %d, dropping record", rec.Tick)
	}
}

// maxBurstPerTick returns the largest count of records sharing one Tick
// in log. log is monotonic in Tick (the live engine appends records in
// dispatch order, all stamped with the current e.tick), so a single
// linear scan suffices. Returns 0 for an empty log.
func maxBurstPerTick(log []game.InputRecord) int {
	if len(log) == 0 {
		return 0
	}
	maxRun := 1
	run := 1
	for i := 1; i < len(log); i++ {
		if log[i].Tick == log[i-1].Tick {
			run++
			if run > maxRun {
				maxRun = run
			}
		} else {
			run = 1
		}
	}
	return maxRun
}

// forward drains the engine's outCh and pushes envelopes onto the
// viewer-visible OutCh. Drop-on-full so a slow viewer can't stall the
// engine; the next snapshot will catch up.
func (p *Player) forward(ctx context.Context, in <-chan protocol.S2CEnvelope, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-in:
			if !ok {
				return
			}
			select {
			case p.OutCh <- env:
			default:
				// Drop. The viewer is too slow; the next state snapshot
				// will resync. Safe because S2C_STATE is a full
				// reconciliation, not a delta.
			}
		}
	}
}
