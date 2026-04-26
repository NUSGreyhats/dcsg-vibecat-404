package game

import (
	"math/rand"
	"time"

	"github.com/greyhats/defcon-game/server/internal/protocol"
)

// RoomTemplate is the static definition of a room's contents, consumed
// by LoadRoom to produce concrete RoomState + the protocol.RoomData
// shipped to the client. Everything else lives in the tilemap:
//
//   - Initial enemies + reinforcement waves: object-layer markers
//     (parseTilemapEnemiesAndWaves).
//   - Pickups: Health/Ammo point objects (parseTilemapPickups).
//   - Doors:   locked-door tiles on the Walls layer
//     (parseTilemapDoors). TargetRoom is always
//     `currentRoom + 1` (linear progression).
//
// At this point the template is just a tilemap pointer; if there's no
// other per-room scalar to add, this type can collapse to a string map.
type RoomTemplate struct {
	TilemapID string
}

// EnemySpawnDef is the base (pre-RNG-offset) spawn position for an enemy.
type EnemySpawnDef struct {
	EntityID uint32
	Type     uint8
	X, Y     float32
}

// WaveDef is a reinforcement wave — declaration-order in RoomTemplate.Waves
// corresponds to the trigger slot, with WaveTriggers[slot] as the threshold
// as a fraction of initial room population. See docs/specs/features/harder-mobs.md.
type WaveDef struct {
	Mobs []WaveMobDef
}

// WaveMobDef is a single mob within a wave. Entity IDs are allocated at
// fire time — unlike initial enemies, waves do not carry pre-assigned IDs.
type WaveMobDef struct {
	Type uint8
	X, Y float32
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
//
// Doors are derived from locked-door tiles on the Walls layer (see
// parseTilemapDoors). The tile coords + UnlockedGID let the client swap
// the rendered sprite at unlock time without relying on a separate Door
// GameObject.
type DoorDef struct {
	DoorID      uint32
	X, Y        float32
	TargetRoom  uint8
	TileX, TileY int
	UnlockedGID int
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
// true. Returns pointers to the doors that transitioned from
// locked→unlocked this call so the caller can emit S2C_DOOR_UNLOCKED
// broadcasts carrying the tile coords + unlocked GID for sprite swap.
func UnlockDoorsIfClear(r *RoomState) []*DoorState {
	if !r.AllEnemiesDead {
		return nil
	}
	var transitioned []*DoorState
	for _, d := range r.Doors {
		if !d.Unlocked {
			d.Unlocked = true
			transitioned = append(transitioned, d)
		}
	}
	return transitioned
}

// CheckAllEnemiesDead sets r.AllEnemiesDead when every enemy has HP<=0 or
// AIState == AIDead AND every reinforcement wave has fired. A room with
// zero enemies and no unfired waves counts as cleared.
func CheckAllEnemiesDead(r *RoomState) {
	if !AllWavesFired(r) {
		return
	}
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
// are spawned in ascending EntityID order; waves are pre-seeded after all
// initial enemies, in wave-slot order.
//
// `enemies` and `waves` are the room's initial enemy spawns and
// reinforcement waves (production callers pass TilemapEnemies /
// TilemapWaves; tests pass hand-built slices).
//
// Returns a protocol.RoomData suitable for S2C_RUN_STARTED or S2C_ROOM_LOAD.
// Doors always ship as Locked: true — the unlock is a separate broadcast.
func LoadRoom(r *RoomState, rng *rand.Rand, tmpl *RoomTemplate, enemies []EnemySpawnDef, waves []WaveDef, now time.Time) protocol.RoomData {
	// Reset collections for the new room.
	r.Enemies = map[uint32]*EnemyState{}
	r.Pickups = map[uint32]*PickupState{}
	r.Projectiles = map[uint32]*ProjectileState{}
	r.Doors = map[uint32]*DoorState{}
	r.AllEnemiesDead = false
	r.Settled = false
	r.TicksSinceLoad = 0
	r.KillLog = r.KillLog[:0]
	r.PendingWaves = r.PendingWaves[:0]
	r.NextEnemyID = WaveEntityIDBase + uint32(r.RoomIndex)*WaveEntityIDStep

	// Sort ascending by EntityID before SpawnEnemy so RNG consumption
	// stays deterministic across replays.
	ordered := make([]EnemySpawnDef, len(enemies))
	copy(ordered, enemies)
	sortEnemySpawnsByID(ordered)

	enemyOut := make([]protocol.EnemySpawn, 0, len(ordered))
	for _, def := range ordered {
		e := SpawnEnemy(rng, r.RoomIndex, def.EntityID, def.Type, def.X, def.Y, now)
		r.Enemies[def.EntityID] = e
		enemyOut = append(enemyOut, protocol.EnemySpawn{
			EntityID: e.EntityID,
			Type:     e.Type,
			X:        e.X,
			Y:        e.Y,
			HP:       e.HP,
		})
	}

	r.InitialPop = len(ordered)
	seedWaves(r, rng, waves)

	// Pickups are authored exclusively in the tilemap (Health/Ammo
	// point objects on the object layer). See parseTilemapPickups.
	pickups := TilemapPickups(r.RoomIndex)
	pickupOut := make([]protocol.PickupSpawn, 0, len(pickups))
	for _, def := range pickups {
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

	// Doors come from locked-door tiles on the Walls layer (see
	// parseTilemapDoors). The tile coords + UnlockedGID travel with each
	// DoorDef so the client can swap the tile sprite at unlock time.
	doors := TilemapDoors(r.RoomIndex)
	doorOut := make([]protocol.DoorDef, 0, len(doors))
	for _, def := range doors {
		r.Doors[def.DoorID] = &DoorState{
			DoorID:      def.DoorID,
			X:           def.X,
			Y:           def.Y,
			TargetRoom:  def.TargetRoom,
			Unlocked:    false,
			TileX:       def.TileX,
			TileY:       def.TileY,
			UnlockedGID: def.UnlockedGID,
		}
		doorOut = append(doorOut, protocol.DoorDef{
			DoorID:      def.DoorID,
			X:           def.X,
			Y:           def.Y,
			TargetRoom:  def.TargetRoom,
			Locked:      true,
			TileX:       def.TileX,
			TileY:       def.TileY,
			UnlockedGID: def.UnlockedGID,
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
