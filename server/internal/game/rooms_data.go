package game

// Enemy type IDs (mirror of Archetypes keys in mobai.go).
const (
	EnemyGrunt    uint8 = 1
	EnemyRunner   uint8 = 2
	EnemyTank     uint8 = 3
	EnemyKamikaze uint8 = 4
	EnemyBoss     uint8 = 5
)

// RoomTemplates is indexed 1..11. Index 0 is unused; callers that look up
// a room by RunState.CurrentRoom index directly into this array.
//
// Progression:
//
//	Rooms 1–3  — introductory, 2–3 enemies, mostly grunts with occasional runners.
//	Rooms 4–7  — mid-game, 4–6 enemies, add tanks and kamikazes.
//	Rooms 8–10 — late game, dense mixes, Room 10 is the heaviest pre-boss.
//	Room 11    — boss only, no doors; run ends on boss kill.
//
// Doors always target roomIndex+1.
var RoomTemplates = [12]*RoomTemplate{
	nil, // index 0 unused

	// ---- Room 1 — tutorial grunts ----
	//
	// 3 initial mobs, no waves. Tutorial pacing — players learn movement
	// and basic combat without reinforcement pressure.
	{
		TilemapID: "room-1",
		Enemies: []EnemySpawnDef{
			{EntityID: 101, Type: EnemyGrunt, X: 400, Y: 300},
			{EntityID: 102, Type: EnemyGrunt, X: 800, Y: 300},
			{EntityID: 103, Type: EnemyGrunt, X: 600, Y: 420},
		},
		Pickups: []PickupSpawnDef{
			{EntityID: 151, Type: PickupHealthPotion, X: 300, Y: 600},
			{EntityID: 152, Type: PickupAmmoCrate, X: 960, Y: 600},
		},
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
		Enemies: []EnemySpawnDef{
			{EntityID: 201, Type: EnemyGrunt, X: 320, Y: 260},
			{EntityID: 202, Type: EnemyGrunt, X: 960, Y: 260},
			{EntityID: 203, Type: EnemyRunner, X: 640, Y: 180},
			{EntityID: 204, Type: EnemyRunner, X: 640, Y: 450},
		},
		Pickups: []PickupSpawnDef{
			{EntityID: 251, Type: PickupHealthPotion, X: 180, Y: 620},
			{EntityID: 252, Type: PickupAmmoCrate, X: 1100, Y: 620},
		},
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
		Enemies: []EnemySpawnDef{
			{EntityID: 301, Type: EnemyGrunt, X: 300, Y: 240},
			{EntityID: 302, Type: EnemyRunner, X: 900, Y: 240},
			{EntityID: 303, Type: EnemyTank, X: 640, Y: 360},
			{EntityID: 304, Type: EnemyGrunt, X: 480, Y: 460},
		},
		Pickups: []PickupSpawnDef{
			{EntityID: 351, Type: PickupAmmoCrate, X: 200, Y: 640},
			{EntityID: 352, Type: PickupHealthPotion, X: 1080, Y: 640},
		},
		Doors: []DoorDef{
			{DoorID: 391, X: 640, Y: 40, TargetRoom: 4},
		},
	},

	// ---- Room 4 — first kamikaze ----
	//
	// 5 initial mobs: triggers at ≤3 and ≤1.
	{
		TilemapID: "room-4",
		Enemies: []EnemySpawnDef{
			{EntityID: 401, Type: EnemyGrunt, X: 280, Y: 220},
			{EntityID: 402, Type: EnemyGrunt, X: 1000, Y: 220},
			{EntityID: 403, Type: EnemyRunner, X: 640, Y: 200},
			{EntityID: 404, Type: EnemyKamikaze, X: 400, Y: 360},
			{EntityID: 405, Type: EnemyKamikaze, X: 880, Y: 360},
		},
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
		Pickups: []PickupSpawnDef{
			{EntityID: 451, Type: PickupHealthPotion, X: 180, Y: 640},
			{EntityID: 452, Type: PickupAmmoCrate, X: 1100, Y: 640},
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
		Enemies: []EnemySpawnDef{
			{EntityID: 501, Type: EnemyRunner, X: 260, Y: 200},
			{EntityID: 502, Type: EnemyRunner, X: 1020, Y: 200},
			{EntityID: 503, Type: EnemyTank, X: 640, Y: 320},
			{EntityID: 504, Type: EnemyGrunt, X: 420, Y: 440},
			{EntityID: 505, Type: EnemyGrunt, X: 860, Y: 440},
		},
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
		Pickups: []PickupSpawnDef{
			{EntityID: 551, Type: PickupAmmoCrate, X: 200, Y: 640},
			{EntityID: 552, Type: PickupHealthPotion, X: 1080, Y: 640},
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
		Enemies: []EnemySpawnDef{
			{EntityID: 601, Type: EnemyTank, X: 420, Y: 260},
			{EntityID: 602, Type: EnemyTank, X: 860, Y: 260},
			{EntityID: 603, Type: EnemyRunner, X: 640, Y: 200},
			{EntityID: 604, Type: EnemyGrunt, X: 280, Y: 440},
			{EntityID: 605, Type: EnemyGrunt, X: 1000, Y: 440},
			{EntityID: 606, Type: EnemyKamikaze, X: 640, Y: 500},
		},
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
		Pickups: []PickupSpawnDef{
			{EntityID: 651, Type: PickupHealthPotion, X: 220, Y: 640},
			{EntityID: 652, Type: PickupAmmoCrate, X: 1060, Y: 640},
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
		Enemies: []EnemySpawnDef{
			{EntityID: 701, Type: EnemyRunner, X: 240, Y: 220},
			{EntityID: 702, Type: EnemyRunner, X: 440, Y: 220},
			{EntityID: 703, Type: EnemyRunner, X: 840, Y: 220},
			{EntityID: 704, Type: EnemyRunner, X: 1040, Y: 220},
			{EntityID: 705, Type: EnemyGrunt, X: 640, Y: 360},
			{EntityID: 706, Type: EnemyKamikaze, X: 640, Y: 480},
		},
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
		Pickups: []PickupSpawnDef{
			{EntityID: 751, Type: PickupHealthPotion, X: 200, Y: 640},
			{EntityID: 752, Type: PickupAmmoCrate, X: 1080, Y: 640},
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
		Enemies: []EnemySpawnDef{
			{EntityID: 801, Type: EnemyGrunt, X: 260, Y: 200},
			{EntityID: 802, Type: EnemyGrunt, X: 1020, Y: 200},
			{EntityID: 803, Type: EnemyTank, X: 640, Y: 280},
			{EntityID: 804, Type: EnemyRunner, X: 420, Y: 400},
			{EntityID: 805, Type: EnemyRunner, X: 860, Y: 400},
			{EntityID: 806, Type: EnemyKamikaze, X: 640, Y: 520},
		},
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
		Pickups: []PickupSpawnDef{
			{EntityID: 851, Type: PickupAmmoCrate, X: 200, Y: 640},
			{EntityID: 852, Type: PickupHealthPotion, X: 1080, Y: 640},
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
		Enemies: []EnemySpawnDef{
			{EntityID: 901, Type: EnemyKamikaze, X: 320, Y: 220},
			{EntityID: 902, Type: EnemyKamikaze, X: 960, Y: 220},
			{EntityID: 903, Type: EnemyRunner, X: 640, Y: 200},
			{EntityID: 904, Type: EnemyTank, X: 420, Y: 360},
			{EntityID: 905, Type: EnemyTank, X: 860, Y: 360},
			{EntityID: 906, Type: EnemyGrunt, X: 640, Y: 480},
		},
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
		Pickups: []PickupSpawnDef{
			{EntityID: 951, Type: PickupHealthPotion, X: 200, Y: 640},
			{EntityID: 952, Type: PickupAmmoCrate, X: 1080, Y: 640},
		},
		Doors: []DoorDef{
			{DoorID: 991, X: 640, Y: 40, TargetRoom: 10},
		},
	},

	// ---- Room 10 — pre-boss heavy ----
	//
	// 8 initial mobs: triggers at ≤5 and ≤2.
	{
		TilemapID: "room-10",
		Enemies: []EnemySpawnDef{
			{EntityID: 1001, Type: EnemyTank, X: 320, Y: 220},
			{EntityID: 1002, Type: EnemyTank, X: 960, Y: 220},
			{EntityID: 1003, Type: EnemyRunner, X: 260, Y: 360},
			{EntityID: 1004, Type: EnemyRunner, X: 1020, Y: 360},
			{EntityID: 1005, Type: EnemyGrunt, X: 480, Y: 320},
			{EntityID: 1006, Type: EnemyGrunt, X: 800, Y: 320},
			{EntityID: 1007, Type: EnemyKamikaze, X: 420, Y: 500},
			{EntityID: 1008, Type: EnemyKamikaze, X: 860, Y: 500},
		},
		Waves: []WaveDef{
			{Mobs: []WaveMobDef{
				{Type: EnemyTank, X: 640, Y: 560},
				{Type: EnemyRunner, X: 240, Y: 540},
				{Type: EnemyRunner, X: 1040, Y: 540},
				{Type: EnemyKamikaze, X: 640, Y: 200},
			}},
			{Mobs: []WaveMobDef{
				{Type: EnemyTank, X: 320, Y: 200},
				{Type: EnemyTank, X: 960, Y: 200},
				{Type: EnemyGrunt, X: 640, Y: 560},
				{Type: EnemyGrunt, X: 640, Y: 180},
			}},
		},
		Pickups: []PickupSpawnDef{
			{EntityID: 1051, Type: PickupAmmoCrate, X: 200, Y: 640},
			{EntityID: 1052, Type: PickupAmmoCrate, X: 1080, Y: 640},
			{EntityID: 1053, Type: PickupHealthPotion, X: 640, Y: 660},
		},
		Doors: []DoorDef{
			{DoorID: 1091, X: 1220, Y: 384, TargetRoom: 11},
		},
	},

	// ---- Room 11 — boss room. No doors; run ends when boss dies. ----
	//
	// 7 initial mobs: boss at center + a 6-grunt ring at radius 140. Triggers
	// at floor(7*0.66)=4 and floor(7*0.33)=2 living, so both waves keep:
	//   Wave 1 (≤4 living): 10 runners at radius ~300 — mid-distance closers.
	//   Wave 2 (≤2 living): 20 kamikazes hugging the perimeter — punishes
	//                       wall-camping; forces the player back toward the
	//                       boss for the kill.
	// Tier-9+ scaling (×1.6 HP, ×1.2 speed) compounds with melee-damage
	// archetype values — boss feels like a boss; a single bad dodge in
	// wave 2 can chunk half the player's HP.
	{
		TilemapID: "room-11",
		Enemies: []EnemySpawnDef{
			{EntityID: 1101, Type: EnemyBoss, X: 640, Y: 384},
			// 6-grunt ring around the boss at radius 140.
			{EntityID: 1102, Type: EnemyGrunt, X: 780, Y: 384},
			{EntityID: 1103, Type: EnemyGrunt, X: 710, Y: 505},
			{EntityID: 1104, Type: EnemyGrunt, X: 570, Y: 505},
			{EntityID: 1105, Type: EnemyGrunt, X: 500, Y: 384},
			{EntityID: 1106, Type: EnemyGrunt, X: 570, Y: 263},
			{EntityID: 1107, Type: EnemyGrunt, X: 710, Y: 263},
		},
		Waves: []WaveDef{
			// Wave 1 — 10 runners at radius ~300, 36° spacing offset 18°
			// so they don't line up with the inner ring.
			{Mobs: []WaveMobDef{
				{Type: EnemyRunner, X: 925, Y: 477},
				{Type: EnemyRunner, X: 816, Y: 627},
				{Type: EnemyRunner, X: 640, Y: 684},
				{Type: EnemyRunner, X: 464, Y: 627},
				{Type: EnemyRunner, X: 355, Y: 477},
				{Type: EnemyRunner, X: 355, Y: 291},
				{Type: EnemyRunner, X: 464, Y: 141},
				{Type: EnemyRunner, X: 640, Y: 84},
				{Type: EnemyRunner, X: 816, Y: 141},
				{Type: EnemyRunner, X: 925, Y: 291},
			}},
			// Wave 2 — 20 kamikazes along the perimeter (7 top, 7 bottom,
			// 3 left, 3 right).
			{Mobs: []WaveMobDef{
				{Type: EnemyKamikaze, X: 120, Y: 80},
				{Type: EnemyKamikaze, X: 320, Y: 80},
				{Type: EnemyKamikaze, X: 520, Y: 80},
				{Type: EnemyKamikaze, X: 640, Y: 80},
				{Type: EnemyKamikaze, X: 760, Y: 80},
				{Type: EnemyKamikaze, X: 960, Y: 80},
				{Type: EnemyKamikaze, X: 1160, Y: 80},
				{Type: EnemyKamikaze, X: 120, Y: 688},
				{Type: EnemyKamikaze, X: 320, Y: 688},
				{Type: EnemyKamikaze, X: 520, Y: 688},
				{Type: EnemyKamikaze, X: 640, Y: 688},
				{Type: EnemyKamikaze, X: 760, Y: 688},
				{Type: EnemyKamikaze, X: 960, Y: 688},
				{Type: EnemyKamikaze, X: 1160, Y: 688},
				{Type: EnemyKamikaze, X: 80, Y: 200},
				{Type: EnemyKamikaze, X: 80, Y: 384},
				{Type: EnemyKamikaze, X: 80, Y: 568},
				{Type: EnemyKamikaze, X: 1200, Y: 200},
				{Type: EnemyKamikaze, X: 1200, Y: 384},
				{Type: EnemyKamikaze, X: 1200, Y: 568},
			}},
		},
		Pickups: []PickupSpawnDef{
			{EntityID: 1151, Type: PickupHealthPotion, X: 200, Y: 600},
			{EntityID: 1152, Type: PickupHealthPotion, X: 1080, Y: 600},
			{EntityID: 1153, Type: PickupAmmoCrate, X: 320, Y: 384},
			{EntityID: 1154, Type: PickupAmmoCrate, X: 960, Y: 384},
			{EntityID: 1155, Type: PickupAmmoCrate, X: 640, Y: 600},
		},
		Doors: nil,
	},
}
