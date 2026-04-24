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
// Room 11 is the only room with a Heart (pickup type 5). Doors always target
// roomIndex+1.
var RoomTemplates = [12]*RoomTemplate{
	nil, // index 0 unused

	// ---- Room 1 — tutorial grunts ----
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
			{EntityID: 252, Type: PickupSpeedBoost, X: 1100, Y: 620},
		},
		Doors: []DoorDef{
			{DoorID: 291, X: 1220, Y: 384, TargetRoom: 3},
		},
	},

	// ---- Room 3 — first tank ----
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
			{EntityID: 352, Type: PickupDamageBoost, X: 1080, Y: 640},
		},
		Doors: []DoorDef{
			{DoorID: 391, X: 640, Y: 40, TargetRoom: 4},
		},
	},

	// ---- Room 4 — first kamikaze ----
	{
		TilemapID: "room-4",
		Enemies: []EnemySpawnDef{
			{EntityID: 401, Type: EnemyGrunt, X: 280, Y: 220},
			{EntityID: 402, Type: EnemyGrunt, X: 1000, Y: 220},
			{EntityID: 403, Type: EnemyRunner, X: 640, Y: 200},
			{EntityID: 404, Type: EnemyKamikaze, X: 400, Y: 360},
			{EntityID: 405, Type: EnemyKamikaze, X: 880, Y: 360},
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
	{
		TilemapID: "room-5",
		Enemies: []EnemySpawnDef{
			{EntityID: 501, Type: EnemyRunner, X: 260, Y: 200},
			{EntityID: 502, Type: EnemyRunner, X: 1020, Y: 200},
			{EntityID: 503, Type: EnemyTank, X: 640, Y: 320},
			{EntityID: 504, Type: EnemyGrunt, X: 420, Y: 440},
			{EntityID: 505, Type: EnemyGrunt, X: 860, Y: 440},
		},
		Pickups: []PickupSpawnDef{
			{EntityID: 551, Type: PickupSpeedBoost, X: 200, Y: 640},
			{EntityID: 552, Type: PickupHealthPotion, X: 1080, Y: 640},
		},
		Doors: []DoorDef{
			{DoorID: 591, X: 1220, Y: 384, TargetRoom: 6},
		},
	},

	// ---- Room 6 — tank pair ----
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
		Pickups: []PickupSpawnDef{
			{EntityID: 651, Type: PickupDamageBoost, X: 220, Y: 640},
			{EntityID: 652, Type: PickupAmmoCrate, X: 1060, Y: 640},
		},
		Doors: []DoorDef{
			{DoorID: 691, X: 640, Y: 40, TargetRoom: 7},
		},
	},

	// ---- Room 7 — runner swarm ----
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
		Pickups: []PickupSpawnDef{
			{EntityID: 751, Type: PickupHealthPotion, X: 200, Y: 640},
			{EntityID: 752, Type: PickupSpeedBoost, X: 1080, Y: 640},
		},
		Doors: []DoorDef{
			{DoorID: 791, X: 1220, Y: 384, TargetRoom: 8},
		},
	},

	// ---- Room 8 — mixed late game ----
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
		Pickups: []PickupSpawnDef{
			{EntityID: 851, Type: PickupAmmoCrate, X: 200, Y: 640},
			{EntityID: 852, Type: PickupDamageBoost, X: 1080, Y: 640},
		},
		Doors: []DoorDef{
			{DoorID: 891, X: 1220, Y: 384, TargetRoom: 9},
		},
	},

	// ---- Room 9 — kamikaze gauntlet ----
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
		Pickups: []PickupSpawnDef{
			{EntityID: 951, Type: PickupHealthPotion, X: 200, Y: 640},
			{EntityID: 952, Type: PickupSpeedBoost, X: 1080, Y: 640},
		},
		Doors: []DoorDef{
			{DoorID: 991, X: 640, Y: 40, TargetRoom: 10},
		},
	},

	// ---- Room 10 — pre-boss heavy ----
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
		Pickups: []PickupSpawnDef{
			{EntityID: 1051, Type: PickupAmmoCrate, X: 200, Y: 640},
			{EntityID: 1052, Type: PickupDamageBoost, X: 1080, Y: 640},
			{EntityID: 1053, Type: PickupHealthPotion, X: 640, Y: 660},
		},
		Doors: []DoorDef{
			{DoorID: 1091, X: 1220, Y: 384, TargetRoom: 11},
		},
	},

	// ---- Room 11 — boss room. No doors; run ends when boss dies. ----
	{
		TilemapID: "room-11",
		Enemies: []EnemySpawnDef{
			{EntityID: 1101, Type: EnemyBoss, X: 640, Y: 280},
		},
		Pickups: []PickupSpawnDef{
			{EntityID: 1151, Type: PickupHeart, X: 640, Y: 600},
			{EntityID: 1152, Type: PickupAmmoCrate, X: 200, Y: 600},
			{EntityID: 1153, Type: PickupDamageBoost, X: 1080, Y: 600},
		},
		Doors: nil,
	},
}
