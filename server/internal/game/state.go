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
	Armor          int16
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
}

func NewRoomState(idx uint8) *RoomState {
	return &RoomState{
		RoomIndex:   idx,
		Enemies:     map[uint32]*EnemyState{},
		Pickups:     map[uint32]*PickupState{},
		Projectiles: map[uint32]*ProjectileState{},
		Doors:       map[uint32]*DoorState{},
		NextProjID:  1,
	}
}
