package game

import "time"

const (
	TickHz            = 20
	TickDuration      = 50 * time.Millisecond
	RoomWidth         = 1280
	RoomHeight        = 768
	FinalRoom   uint8 = 10
	BossRoom    uint8 = 10

	SwapCooldownMs = 400

	// BUG-05: square-axis hitbox with a grossly-wrong range constant.
	InteractRange = 2500

	SettleTicks = 2

	PlayerSpeed       float32 = 220
	ProjectileSpeed   float32 = 600
	ProjectileLifeSec float32 = 1.5

	MaxPlayerHP int16 = 100

	// Replay window: C2S_START_RUN seed must fall within ±30 min of server now.
	SeedWindow = 30 * time.Minute

	// Per-packet ts validation window. No cumulative drift check — BUG-02.
	TsValidationWindow = 2 * time.Second

	// MaxRunDuration is the hard cap on a single run. The lobby advancer's
	// TurnTTL is 5min; this gives ~2× margin for slow-but-legitimate play
	// before we force-end the run as RunFailed and release the
	// MAX_CONCURRENT slot held by handleWS for waiting players.
	MaxRunDuration = 10 * time.Minute

	// IdleTimeout is the no-C2S-packet window after which an active run is
	// force-ended as RunFailed. Catches AFK / disconnected-but-not-closed
	// clients much faster than MaxRunDuration. Set on a per-engine clock
	// (Engine.lastInputAt), reset in drainInput on every received frame.
	IdleTimeout = 90 * time.Second

	// Reconciliation tick cadence (BUG-11 recovery).
	ReconcileEveryTicks = 10

	InChBuf  = 64
	OutChBuf = 64

	// --- Harder-mobs tuning. See docs/specs/features/harder-mobs.md. ---

	// WaveArmDelayMs is the base delay from wave arm → fire.
	WaveArmDelayMs = 1500

	// WaveArmJitterMs is the ± jitter applied to each wave's arm→fire delay.
	// Consumes one MobRNG.Intn(2 * WaveArmJitterMs) per declared wave at
	// room load. Changing this value is a determinism change — old seeds
	// invalidated.
	WaveArmJitterMs = 1500

	// WaveEntityIDBase is the starting EntityID for mobs spawned by
	// waves (template mob IDs stay well below this). Per-room offset keeps
	// wave IDs unique across rooms for cleaner logs.
	WaveEntityIDBase uint32 = 100000
	WaveEntityIDStep uint32 = 1000

	// PackAggroRadius is the pixel radius for on-damage aggro broadcast.
	// No LOS check. See docs/specs/features/harder-mobs.md.
	PackAggroRadius float32 = 250.0
)

// WaveTriggers is the ordered set of relative thresholds (fraction of initial
// room population) at which each wave arms. WaveTriggers[i] corresponds to
// wave slot i in a room's template. A room may declare 0..len(WaveTriggers)
// waves.
//
// Changing this array is both a balance change and a determinism change —
// old seeds invalidated (skip rule may gate different waves).
var WaveTriggers = []float32{0.66, 0.33}
