package leaderboard

// Entry mirrors the JSON shape of protocol.LeaderboardEntry on the game
// server. Defined locally so the lobby doesn't depend on the WS protocol
// package; the public /api/leaderboard contract lives here.
type Entry struct {
	Rank        int     `json:"rank"`
	DisplayName string  `json:"display_name"`
	TotalMs     int64   `json:"total_ms"`
	Splits      []Split `json:"splits"`
	Seed        int64   `json:"seed"`
	ReplayID    string  `json:"replay_id"`
	SubmittedAt string  `json:"submitted_at"`
}

// Split mirrors protocol.SplitData. See
// server/internal/protocol/packets.go for the source of truth.
type Split struct {
	RoomIndex uint8 `json:"room_index"`
	TimeMs    int64 `json:"time_ms"`
	Kills     uint8 `json:"kills"`
}
