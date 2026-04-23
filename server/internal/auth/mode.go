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
	// ModeToken validates player_id against the allowlist (prod).
	ModeToken Mode = 1
)

// ModeFromEnv reads AUTH_MODE from the environment. "token" → ModeToken,
// anything else (including unset) → ModeOpen.
func ModeFromEnv() Mode {
	v := strings.ToLower(os.Getenv("AUTH_MODE"))
	if v == "token" {
		return ModeToken
	}
	return ModeOpen
}
