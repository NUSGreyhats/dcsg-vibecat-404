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
}

type EnterDoorPacket struct {
	DoorID uint32 `json:"door_id"`
}

type UseItemPacket struct {
	ItemName string `json:"item_name"`
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
	X         float32  `json:"x"`
	Y         float32  `json:"y"`
	HP        int16    `json:"hp"`
	WeaponID  uint8    `json:"weapon_id"`
	Ammo      int      `json:"ammo"`
	Inventory []string `json:"inventory"`
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
}

type RoomLoadData struct {
	RoomIndex uint8    `json:"room_index"`
	Room      RoomData `json:"room"`
	SplitMs   int64    `json:"split_ms"`
}

type RunCompleteData struct {
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
