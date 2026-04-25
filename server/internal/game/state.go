package game

import (
	"math/rand"
	"time"

	"github.com/greyhats/defcon-game/server/internal/protocol"
)

type RunStatus uint8

const (
	RunIdle RunStatus = iota
	RunRunning
	RunCompleted
	RunFailed
)

// IsTerminal reports whether the run is in an end state (the tickLoop
// should exit, no further input is accepted).
func (s RunStatus) IsTerminal() bool {
	return s == RunCompleted || s == RunFailed
}

// Outcome returns the wire-format outcome for a terminal status. Panics on
// non-terminal values — callers must gate on IsTerminal first.
func (s RunStatus) Outcome() protocol.RunOutcome {
	switch s {
	case RunCompleted:
		return protocol.RunOutcomeCompleted
	case RunFailed:
		return protocol.RunOutcomeFailed
	}
	panic("RunStatus.Outcome called on non-terminal status")
}

type Vec2 struct {
	X, Y float32
}

type AIState uint8

const (
	AIIdle AIState = iota
	AIPatrol
	AIAggro
	AIDead
)

type InputRecord struct {
	Tick uint64 `json:"t"`
	Type uint8  `json:"k"`
	Seq  uint32 `json:"s"`
	Ts   int64  `json:"ts"`
	Data []byte `json:"d"`
}

type KillRecord struct {
	EntityID  uint32
	Timestamp time.Time
}

type RunState struct {
	Status       RunStatus
	PlayerID     string
	DisplayName  string
	RunStartTime time.Time
	Seed         int64
	MobRNG       *rand.Rand
	CurrentRoom  uint8
	ElapsedMs    int64
	Splits       []protocol.SplitData
	InputLog     []InputRecord
	RunsUsed     uint8
}

type PlayerState struct {
	X, Y            float32
	VelX, VelY      float32
	HP, MaxHP       int16
	CurrentWeapon   uint8
	UnlockedWeapons []uint8
	WeaponCooldown  map[uint8]time.Time
	WeaponAmmo      map[uint8]int
	SwapCooldownEnd time.Time
	DamageTaken     int32
	Inventory       []string

	SpeedMultUntil  time.Time
	SpeedMult       float32
	DamageMultUntil time.Time
	DamageMult      float32
	ShieldUntil     time.Time

	// Shop traits — permanent stackable buffs purchased from features/shop.md.
	// Reset on death / new run. Effective multipliers are derived at read
	// time, never cached on this struct. The shop's `ammo` purchase is a
	// one-shot refill (not stackable) so there is no AmmoStacks counter.
	Coins          int32
	SpeedStacks    uint8
	FireRateStacks uint8
	DamageStacks   uint8
}

func NewPlayerState() *PlayerState {
	p := &PlayerState{
		X:               RoomWidth / 2,
		Y:               RoomHeight - 100,
		HP:              MaxPlayerHP,
		MaxHP:           MaxPlayerHP,
		CurrentWeapon:   1,
		UnlockedWeapons: []uint8{1, 2, 3},
		WeaponCooldown:  map[uint8]time.Time{},
		WeaponAmmo:      map[uint8]int{},
		Inventory:       []string{},
		SpeedMult:       1,
		DamageMult:      1,
	}
	for id, spec := range WeaponSpecs {
		p.WeaponAmmo[id] = spec.AmmoStart
	}
	return p
}

type EnemyState struct {
	EntityID       uint32
	Type           uint8
	X, Y           float32
	HP, MaxHP      int16
	AIState        AIState
	PatrolPath     []Vec2
	PatrolIndex    uint8
	AggroRange     float32
	AttackCooldown time.Time
	Speed          float32
	JitterX        float32
	JitterY        float32
}

type ProjectileState struct {
	ProjID     uint32
	X, Y       float32
	VelX, VelY float32
	Damage     int16
	OwnerID    uint32
	SpawnedAt  time.Time
}

type PickupState struct {
	EntityID uint32
	Type     uint8
	X, Y     float32
	Consumed bool
	// Despawn deadline. Zero value means "never expires" (room-templated
	// pickups). Coins set this 30s after spawn — see features/shop.md.
	ExpiresAt time.Time
}

type DoorState struct {
	DoorID     uint32
	X, Y       float32
	TargetRoom uint8
	Unlocked   bool
}

type RoomState struct {
	RoomIndex      uint8
	Enemies        map[uint32]*EnemyState
	Pickups        map[uint32]*PickupState
	Projectiles    map[uint32]*ProjectileState
	Doors          map[uint32]*DoorState
	AllEnemiesDead bool
	Settled        bool
	TicksSinceLoad uint8
	KillLog        []KillRecord
	// Allocation counter for projectile IDs within the room.
	NextProjID uint32

	// Allocation counter for runtime-spawned non-enemy entity IDs (coin
	// drops). Lazily initialised to 1_000_000 — well above WaveEntityIDBase
	// (100_000) + max idx*step, so it cannot collide with wave-spawned IDs.
	nextEntityID uint32

	// --- Harder-mobs state. See docs/specs/features/harder-mobs.md. ---

	// InitialPop is the initial enemy count captured at room load. Used as
	// the denominator for per-wave trigger thresholds.
	InitialPop int

	// PendingWaves holds one entry per declared wave whose threshold
	// rounded to ≥1 living remaining. Skipped waves consume no RNG and
	// produce no PendingWave entry.
	PendingWaves []PendingWave

	// NextEnemyID allocates IDs for wave-spawned mobs at fire time.
	NextEnemyID uint32
}

// NextEntityID returns a fresh uint32 for runtime-spawned non-enemy
// entities (coin drops). Counters are room-local; collisions across rooms
// are fine because the client wipes its entity maps on every ROOM_LOAD.
func (r *RoomState) NextEntityID() uint32 {
	if r.nextEntityID == 0 {
		r.nextEntityID = 1_000_000
	}
	r.nextEntityID++
	return r.nextEntityID
}

// MobSeed holds the post-RNG, pre-instantiation data for a mob. All RNG is
// consumed into this struct at room load so no RNG is needed at wave fire.
type MobSeed struct {
	SpawnX, SpawnY   float32 // wall-snapped
	PatrolPath       []Vec2  // wall-snapped
	JitterX, JitterY float32
	FirstAttackDelay int // ms, added to the instantiation `now`
}

// PrecomputedMob pairs a seeded mob with its archetype, ready to be turned
// into an EnemyState at wave fire without touching MobRNG.
type PrecomputedMob struct {
	Type uint8
	Seed MobSeed
}

// PendingWave tracks one reinforcement wave through its arm/fire lifecycle.
type PendingWave struct {
	TriggerRemaining int              // arms when livingCount ≤ this
	ArmDelayJitter   int              // ms, signed offset from WaveArmDelayMs
	Mobs             []PrecomputedMob // pre-seeded; instantiated on fire
	Armed            bool             // one-way latch
	FireAt           time.Time        // set when Armed flips true
	Fired            bool             // one-way latch
}

func NewRoomState(idx uint8) *RoomState {
	return &RoomState{
		RoomIndex:   idx,
		Enemies:     map[uint32]*EnemyState{},
		Pickups:     map[uint32]*PickupState{},
		Projectiles: map[uint32]*ProjectileState{},
		Doors:       map[uint32]*DoorState{},
		NextProjID:  1,
		NextEnemyID: WaveEntityIDBase + uint32(idx)*WaveEntityIDStep,
	}
}
