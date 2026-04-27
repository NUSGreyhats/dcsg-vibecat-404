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
	Current     bool    `json:"current_player,omitempty"`
}

// Page is a ranked slice of the public leaderboard plus enough metadata
// for the browser to render pagination controls without guessing.
type Page struct {
	Entries []Entry `json:"entries"`
	Page    int     `json:"page"`
	Limit   int     `json:"limit"`
	Total   int     `json:"total"`
	HasPrev bool    `json:"has_prev"`
	HasNext bool    `json:"has_next"`
}

// Split mirrors protocol.SplitData. See
// server/internal/protocol/packets.go for the source of truth.
type Split struct {
	RoomIndex uint8 `json:"room_index"`
	TimeMs    int64 `json:"time_ms"`
	Kills     uint8 `json:"kills"`
}
