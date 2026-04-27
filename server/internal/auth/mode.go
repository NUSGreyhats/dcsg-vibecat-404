package auth

import (
	"fmt"
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

// ModeFromEnv reads AUTH_MODE from the environment. Recognized values:
// "open", "token", "identity". Empty or unrecognized values return an
// error so a typo (AUTH_MODE=tokne) fails closed at startup instead of
// silently downgrading to ModeOpen and disabling auth in prod.
func ModeFromEnv() (Mode, error) {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("AUTH_MODE")))
	switch v {
	case "open":
		return ModeOpen, nil
	case "token":
		return ModeToken, nil
	case "identity":
		return ModeIdentity, nil
	case "":
		return 0, fmt.Errorf("AUTH_MODE not set: must be one of open, token, identity")
	default:
		return 0, fmt.Errorf("AUTH_MODE=%q invalid: must be one of open, token, identity", v)
	}
}
