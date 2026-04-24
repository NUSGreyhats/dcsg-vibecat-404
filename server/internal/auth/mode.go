package auth

import (
	"os"
	"strings"
)

// Mode gates WebSocket authentication at C2S_AUTH time.
// See docs/specs/foundations/03-identity-auth.md.
type Mode uint8

const (
	// ModeOpen accepts any player_id (dev / test).
	ModeOpen Mode = 0
	// ModeToken validates player_id against the allowlist (prod queue).
	ModeToken Mode = 1
	// ModeIdentity validates player_id against the identity.db players
	// table — i.e. requires the player to have registered.
	ModeIdentity Mode = 2
)

// ModeFromEnv reads AUTH_MODE from the environment. "token" → ModeToken,
// "identity" → ModeIdentity, anything else (including unset) → ModeOpen.
func ModeFromEnv() Mode {
	switch strings.ToLower(os.Getenv("AUTH_MODE")) {
	case "token":
		return ModeToken
	case "identity":
		return ModeIdentity
	default:
		return ModeOpen
	}
}
