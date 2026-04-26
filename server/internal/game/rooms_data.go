package game

// Enemy type IDs (mirror of Archetypes keys in mobai.go).
const (
	EnemyGrunt    uint8 = 1
	EnemyRunner   uint8 = 2
	EnemyTank     uint8 = 3
	EnemyKamikaze uint8 = 4
	EnemyBoss     uint8 = 5
)

// RoomTemplates is indexed 1..10. Index 0 is unused; callers that look up
// a room by RunState.CurrentRoom index directly into this array.
//
// Progression:
//
//	Rooms 1–3  — introductory, 2–3 enemies, mostly grunts with occasional runners.
//	Rooms 4–7  — mid-game, 4–6 enemies, add tanks and kamikazes.
//	Rooms 8–9  — late game, dense mixes.
//	Room 10    — boss room (boss + ring + thematic waves), no doors;
//	             run ends on boss kill.
//
// Doors always target roomIndex+1.
var RoomTemplates = [11]*RoomTemplate{
	nil, // index 0 unused

	// ---- Room 1 — tutorial grunts ----
	//
	// 3 initial mobs, no waves. Tutorial pacing — players learn movement
	// and basic combat without reinforcement pressure.
	{
		TilemapID: "room-1",
		Doors: []DoorDef{
			{DoorID: 191, X: 1220, Y: 384, TargetRoom: 2},
		},
	},

	// ---- Room 2 — introduce runners ----
	//
	// 4 initial mobs, no waves. Players learn the runner archetype on a
	// clean fight before waves enter the picture in room 4+.
	{
		TilemapID: "room-2",
		Doors: []DoorDef{
			{DoorID: 291, X: 1220, Y: 384, TargetRoom: 3},
		},
	},

	// ---- Room 3 — first tank ----
	//
	// 4 initial mobs, no waves. Introduces the tank archetype; pacing
	// still in tutorial mode.
	{
		TilemapID: "room-3",
		Doors: []DoorDef{
			{DoorID: 391, X: 640, Y: 40, TargetRoom: 4},
		},
	},

	// ---- Room 4 — first kamikaze ----
	//
	// 5 initial mobs: triggers at ≤3 and ≤1.
	{
		TilemapID: "room-4",
		Waves: []WaveDef{
			{Mobs: []WaveMobDef{
				{Type: EnemyKamikaze, X: 240, Y: 500},
				{Type: EnemyKamikaze, X: 1040, Y: 500},
			}},
			{Mobs: []WaveMobDef{
				{Type: EnemyGrunt, X: 320, Y: 180},
				{Type: EnemyGrunt, X: 960, Y: 180},
				{Type: EnemyRunner, X: 640, Y: 540},
			}},
		},
		Doors: []DoorDef{
			{DoorID: 491, X: 1220, Y: 384, TargetRoom: 5},
		},
	},

	// ---- Room 5 — mid-game pressure ----
	//
	// 5 initial mobs: triggers at ≤3 and ≤1.
	{
		TilemapID: "room-5",
		Waves: []WaveDef{
			{Mobs: []WaveMobDef{
				{Type: EnemyRunner, X: 200, Y: 500},
				{Type: EnemyRunner, X: 1080, Y: 500},
			}},
			{Mobs: []WaveMobDef{
				{Type: EnemyTank, X: 640, Y: 500},
				{Type: EnemyGrunt, X: 640, Y: 200},
			}},
		},
		Doors: []DoorDef{
			{DoorID: 591, X: 1220, Y: 384, TargetRoom: 6},
		},
	},

	// ---- Room 6 — tank pair ----
	//
	// 6 initial mobs: triggers at ≤3 and ≤1.
	{
		TilemapID: "room-6",
		Waves: []WaveDef{
			{Mobs: []WaveMobDef{
				{Type: EnemyKamikaze, X: 320, Y: 540},
				{Type: EnemyKamikaze, X: 960, Y: 540},
			}},
			{Mobs: []WaveMobDef{
				{Type: EnemyTank, X: 640, Y: 540},
				{Type: EnemyRunner, X: 260, Y: 180},
				{Type: EnemyRunner, X: 1020, Y: 180},
			}},
		},
		Doors: []DoorDef{
			{DoorID: 691, X: 640, Y: 40, TargetRoom: 7},
		},
	},

	// ---- Room 7 — runner swarm ----
	//
	// 6 initial mobs: triggers at ≤3 and ≤1.
	{
		TilemapID: "room-7",
		Waves: []WaveDef{
			{Mobs: []WaveMobDef{
				{Type: EnemyRunner, X: 320, Y: 540},
				{Type: EnemyRunner, X: 960, Y: 540},
			}},
			{Mobs: []WaveMobDef{
				{Type: EnemyTank, X: 640, Y: 540},
				{Type: EnemyRunner, X: 240, Y: 400},
				{Type: EnemyRunner, X: 1040, Y: 400},
			}},
		},
		Doors: []DoorDef{
			{DoorID: 791, X: 1220, Y: 384, TargetRoom: 8},
		},
	},

	// ---- Room 8 — mixed late game ----
	//
	// 6 initial mobs: triggers at ≤3 and ≤1.
	{
		TilemapID: "room-8",
		Waves: []WaveDef{
			{Mobs: []WaveMobDef{
				{Type: EnemyRunner, X: 220, Y: 500},
				{Type: EnemyRunner, X: 1060, Y: 500},
				{Type: EnemyKamikaze, X: 640, Y: 540},
			}},
			{Mobs: []WaveMobDef{
				{Type: EnemyTank, X: 640, Y: 180},
				{Type: EnemyGrunt, X: 320, Y: 540},
				{Type: EnemyGrunt, X: 960, Y: 540},
			}},
		},
		Doors: []DoorDef{
			{DoorID: 891, X: 1220, Y: 384, TargetRoom: 9},
		},
	},

	// ---- Room 9 — kamikaze gauntlet ----
	//
	// 6 initial mobs: triggers at ≤3 and ≤1.
	{
		TilemapID: "room-9",
		Waves: []WaveDef{
			{Mobs: []WaveMobDef{
				{Type: EnemyKamikaze, X: 200, Y: 520},
				{Type: EnemyKamikaze, X: 1080, Y: 520},
				{Type: EnemyRunner, X: 640, Y: 540},
			}},
			{Mobs: []WaveMobDef{
				{Type: EnemyTank, X: 640, Y: 540},
				{Type: EnemyKamikaze, X: 240, Y: 180},
				{Type: EnemyKamikaze, X: 1040, Y: 180},
			}},
		},
		Doors: []DoorDef{
			{DoorID: 991, X: 640, Y: 40, TargetRoom: 10},
		},
	},

	// ---- Room 10 — boss room. No doors; run ends when boss dies. ----
	//
	// 12 initial mobs: pre-boss heavy (8 tanks/runners/grunts/kamikazes)
	// plus boss + 3-grunt ring (radius 140, 120° spacing). Initial
	// positions live in the tilemap. Triggers at floor(12*0.66)=7 and
	// floor(12*0.33)=3 living:
	//   Wave 1 (≤7 living): 5 runners at radius ~300, 72° spacing —
	//                       mid-distance closers.
	//   Wave 2 (≤3 living): 10 kamikazes hugging the perimeter — punishes
	//                       wall-camping; forces the player back toward
	//                       the boss for the kill.
	// Tier-9+ scaling (×1.6 HP, ×1.2 speed) compounds with melee-damage
	// archetype values — boss feels like a boss; a single bad dodge in
	// wave 2 can chunk half the player's HP.
	{
		TilemapID: "room-10",
		Waves: []WaveDef{
			// Wave 1 — 5 runners at radius ~300, 72° spacing.
			{Mobs: []WaveMobDef{
				{Type: EnemyRunner, X: 925, Y: 477},
				{Type: EnemyRunner, X: 640, Y: 684},
				{Type: EnemyRunner, X: 355, Y: 477},
				{Type: EnemyRunner, X: 464, Y: 141},
				{Type: EnemyRunner, X: 816, Y: 141},
			}},
			// Wave 2 — 10 kamikazes along the perimeter.
			{Mobs: []WaveMobDef{
				{Type: EnemyKamikaze, X: 120, Y: 80},
				{Type: EnemyKamikaze, X: 520, Y: 80},
				{Type: EnemyKamikaze, X: 760, Y: 80},
				{Type: EnemyKamikaze, X: 1160, Y: 80},
				{Type: EnemyKamikaze, X: 320, Y: 688},
				{Type: EnemyKamikaze, X: 640, Y: 688},
				{Type: EnemyKamikaze, X: 960, Y: 688},
				{Type: EnemyKamikaze, X: 80, Y: 384},
				{Type: EnemyKamikaze, X: 1200, Y: 200},
				{Type: EnemyKamikaze, X: 1200, Y: 568},
			}},
		},
		Doors: nil,
	},
}
