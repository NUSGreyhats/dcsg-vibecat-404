package game

import (
	"math/rand"
	"time"

	"github.com/greyhats/defcon-game/server/internal/protocol"
)

// RoomTemplate is the static definition of a room's contents, consumed by
// LoadRoom to produce concrete RoomState + the protocol.RoomData shipped to
// the client.
type RoomTemplate struct {
	TilemapID string
	Enemies   []EnemySpawnDef
	Pickups   []PickupSpawnDef
	Doors     []DoorDef
}

// EnemySpawnDef is the base (pre-RNG-offset) spawn position for an enemy.
type EnemySpawnDef struct {
	EntityID uint32
	Type     uint8
	X, Y     float32
}

// PickupSpawnDef is a static pickup placement.
type PickupSpawnDef struct {
	EntityID uint32
	Type     uint8
	X, Y     float32
}

// DoorDef is a static door placement. Doors always ship to the client as
// Locked: true; the S2C_DOOR_UNLOCKED broadcast flips the client's view
// once the server's unlock pass fires.
type DoorDef struct {
	DoorID     uint32
	X, Y       float32
	TargetRoom uint8
}

// HandleEnterDoor validates that a door exists in the current room and is
// Unlocked, then returns the target room. BUG-03: there is deliberately no
// player-to-door distance check.
func HandleEnterDoor(p *PlayerState, r *RoomState, doorID uint32) (nextRoom uint8, ok bool) {
	_ = p
	door, exists := r.Doors[doorID]
	if !exists {
		return 0, false
	}
	if !door.Unlocked {
		return 0, false
	}
	return door.TargetRoom, true
}

// UnlockDoorsIfClear flips every door to Unlocked when AllEnemiesDead is
// true. Returns the slice of door IDs that transitioned from locked→unlocked
// this call so the caller can emit S2C_DOOR_UNLOCKED broadcasts.
func UnlockDoorsIfClear(r *RoomState) []uint32 {
	if !r.AllEnemiesDead {
		return nil
	}
	var transitioned []uint32
	for _, d := range r.Doors {
		if !d.Unlocked {
			d.Unlocked = true
			transitioned = append(transitioned, d.DoorID)
		}
	}
	return transitioned
}

// CheckAllEnemiesDead sets r.AllEnemiesDead when every enemy has HP<=0 or
// AIState == AIDead. A room with zero enemies counts as cleared.
func CheckAllEnemiesDead(r *RoomState) {
	for _, e := range r.Enemies {
		if e.HP > 0 && e.AIState != AIDead {
			return
		}
	}
	r.AllEnemiesDead = true
}

// LoadRoom populates r.Enemies / r.Pickups / r.Doors from the template.
// Enemies go through SpawnEnemy so RNG consumption matches the order
// mandated by docs/specs/foundations/07-determinism-seeding.md. Enemies
// are spawned in ascending EntityID order.
//
// Returns a protocol.RoomData suitable for S2C_RUN_STARTED or S2C_ROOM_LOAD.
// Doors always ship as Locked: true — the unlock is a separate broadcast.
func LoadRoom(r *RoomState, rng *rand.Rand, tmpl *RoomTemplate, now time.Time) protocol.RoomData {
	// Reset collections for the new room.
	r.Enemies = map[uint32]*EnemyState{}
	r.Pickups = map[uint32]*PickupState{}
	r.Projectiles = map[uint32]*ProjectileState{}
	r.Doors = map[uint32]*DoorState{}
	r.AllEnemiesDead = false
	r.Settled = false
	r.TicksSinceLoad = 0
	r.KillLog = r.KillLog[:0]

	// Spawn enemies in ascending EntityID order to preserve the RNG
	// consumption invariant.
	ordered := make([]EnemySpawnDef, len(tmpl.Enemies))
	copy(ordered, tmpl.Enemies)
	sortEnemySpawnsByID(ordered)

	enemyOut := make([]protocol.EnemySpawn, 0, len(ordered))
	for _, def := range ordered {
		e := SpawnEnemy(rng, def.EntityID, def.Type, def.X, def.Y, now)
		r.Enemies[def.EntityID] = e
		enemyOut = append(enemyOut, protocol.EnemySpawn{
			EntityID: e.EntityID,
			Type:     e.Type,
			X:        e.X,
			Y:        e.Y,
			HP:       e.HP,
		})
	}

	pickupOut := make([]protocol.PickupSpawn, 0, len(tmpl.Pickups))
	for _, def := range tmpl.Pickups {
		r.Pickups[def.EntityID] = &PickupState{
			EntityID: def.EntityID,
			Type:     def.Type,
			X:        def.X,
			Y:        def.Y,
		}
		pickupOut = append(pickupOut, protocol.PickupSpawn{
			EntityID: def.EntityID,
			Type:     def.Type,
			X:        def.X,
			Y:        def.Y,
		})
	}

	doorOut := make([]protocol.DoorDef, 0, len(tmpl.Doors))
	for _, def := range tmpl.Doors {
		r.Doors[def.DoorID] = &DoorState{
			DoorID:     def.DoorID,
			X:          def.X,
			Y:          def.Y,
			TargetRoom: def.TargetRoom,
			Unlocked:   false,
		}
		doorOut = append(doorOut, protocol.DoorDef{
			DoorID:     def.DoorID,
			X:          def.X,
			Y:          def.Y,
			TargetRoom: def.TargetRoom,
			Locked:     true,
		})
	}

	return protocol.RoomData{
		RoomIndex: r.RoomIndex,
		TilemapID: tmpl.TilemapID,
		Enemies:   enemyOut,
		Pickups:   pickupOut,
		Doors:     doorOut,
	}
}

// sortEnemySpawnsByID is a tiny insertion sort — the enemy list is small
// (single digits per room) so avoiding sort.Slice keeps the stdlib import
// surface minimal.
func sortEnemySpawnsByID(xs []EnemySpawnDef) {
	for i := 1; i < len(xs); i++ {
		j := i
		for j > 0 && xs[j-1].EntityID > xs[j].EntityID {
			xs[j-1], xs[j] = xs[j], xs[j-1]
			j--
		}
	}
}
