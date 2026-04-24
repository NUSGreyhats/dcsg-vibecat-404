package game

import "time"

const (
	TickHz            = 20
	TickDuration      = 50 * time.Millisecond
	RoomWidth         = 1280
	RoomHeight        = 768
	FinalRoom   uint8 = 11
	BossRoom    uint8 = 11

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

	// Reconciliation tick cadence (BUG-11 recovery).
	ReconcileEveryTicks = 10

	InChBuf  = 64
	OutChBuf = 64
)
