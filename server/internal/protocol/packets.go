package protocol

type AuthPacket struct {
	PlayerID string `json:"player_id"`
}

type StartRunPacket struct {
	StartTime int64 `json:"start_time"`
}

type InputPacket struct {
	DX float32 `json:"dx"`
	DY float32 `json:"dy"`
}

type ShootPacket struct {
	AimAngle float32 `json:"aim_angle"`
}

type SwitchWeaponPacket struct {
	WeaponID uint8 `json:"weapon_id"`
}

type InteractPacket struct {
	TargetID uint32 `json:"target_id"`
	// ClaimedType is the pickup type the client says it's interacting
	// with. The server does NOT cross-check this against pk.Type — see
	// BUG-12 in docs/specs/features/shop.md. Old logs decode 0 (no-op).
	ClaimedType uint8 `json:"claimed_type"`
}

type EnterDoorPacket struct {
	DoorID uint32 `json:"door_id"`
}

type UseItemPacket struct {
	ItemName string `json:"item_name"`
}

type ShopPurchasePacket struct {
	ItemID uint8 `json:"item_id"`
}

type EnemySpawn struct {
	EntityID uint32  `json:"entity_id"`
	Type     uint8   `json:"type"`
	X        float32 `json:"x"`
	Y        float32 `json:"y"`
	HP       int16   `json:"hp"`
}

type PickupSpawn struct {
	EntityID uint32  `json:"entity_id"`
	Type     uint8   `json:"type"`
	X        float32 `json:"x"`
	Y        float32 `json:"y"`
}

type DoorDef struct {
	DoorID     uint32  `json:"door_id"`
	X          float32 `json:"x"`
	Y          float32 `json:"y"`
	TargetRoom uint8   `json:"target_room"`
	Locked     bool    `json:"locked"`
}

type RoomData struct {
	RoomIndex uint8         `json:"room_index"`
	TilemapID string        `json:"tilemap_id"`
	Enemies   []EnemySpawn  `json:"enemies"`
	Pickups   []PickupSpawn `json:"pickups"`
	Doors     []DoorDef     `json:"doors"`
}

type PlayerSnapshot struct {
	X              float32  `json:"x"`
	Y              float32  `json:"y"`
	HP             int16    `json:"hp"`
	WeaponID       uint8    `json:"weapon_id"`
	Ammo           int      `json:"ammo"`
	Inventory      []string `json:"inventory"`
	Coins          int32    `json:"coins"`
	SpeedStacks    uint8    `json:"speed_stacks"`
	FireRateStacks uint8    `json:"fire_rate_stacks"`
	DamageStacks   uint8    `json:"damage_stacks"`
	AmmoStacks     uint8    `json:"ammo_stacks"`
}

type EntitySnapshot struct {
	EntityID uint32  `json:"entity_id"`
	Type     uint8   `json:"type"`
	X        float32 `json:"x"`
	Y        float32 `json:"y"`
	HP       int16   `json:"hp"`
	State    uint8   `json:"state"`
}

type ProjSnapshot struct {
	ProjID uint32  `json:"proj_id"`
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Angle  float32 `json:"angle"`
	Owner  uint32  `json:"owner"`
}

type SplitData struct {
	RoomIndex uint8 `json:"room_index"`
	TimeMs    int64 `json:"time_ms"`
	Kills     uint8 `json:"kills"`
}

type LeaderboardEntry struct {
	Rank        int         `json:"rank"`
	DisplayName string      `json:"display_name"`
	TotalMs     int64       `json:"total_ms"`
	Splits      []SplitData `json:"splits"`
	Seed        int64       `json:"seed"`
	ReplayID    string      `json:"replay_id"`
}

type AuthOkData struct {
	DisplayName string `json:"display_name"`
}

type RunStartedData struct {
	Seed int64    `json:"seed"`
	Room RoomData `json:"room"`
}

type StateData struct {
	ElapsedMs   int64            `json:"elapsed_ms"`
	Player      PlayerSnapshot   `json:"player"`
	Entities    []EntitySnapshot `json:"entities"`
	Projectiles []ProjSnapshot   `json:"projectiles"`
	Pickups     []PickupSpawn    `json:"pickups"`
}

type RoomLoadData struct {
	RoomIndex uint8    `json:"room_index"`
	Room      RoomData `json:"room"`
	SplitMs   int64    `json:"split_ms"`
}

// RunOutcome is the wire-format terminal state of a run. Only values listed
// below are valid on the S2C_RUN_COMPLETE packet; see
// docs/specs/foundations/02-protocol.md#s2c_run_complete-outcomes.
type RunOutcome string

const (
	RunOutcomeCompleted RunOutcome = "completed"
	RunOutcomeFailed    RunOutcome = "failed"
)

type RunCompleteData struct {
	Outcome RunOutcome  `json:"outcome"`
	TotalMs int64       `json:"total_ms"`
	Rank    int         `json:"rank"`
	Splits  []SplitData `json:"splits"`
}

type LeaderboardData struct {
	Entries []LeaderboardEntry `json:"entries"`
}

type ErrorData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type DoorUnlockedData struct {
	DoorID uint32 `json:"door_id"`
}

type EntityDiedData struct {
	EntityID uint32 `json:"entity_id"`
}
