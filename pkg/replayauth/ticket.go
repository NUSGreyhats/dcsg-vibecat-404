// Package replayauth issues and verifies the short-lived HMAC tickets
// that gate /ws/replay. The lobby mints tickets from the admin session
// cookie; the game server validates them on WS upgrade. Both sides use
// ADMIN_SESSION_SECRET so no extra env var is needed.
//
// Ticket format:
//
//	<replay_id>.<exp_unix>.<base64url(hmac_sha256(secret, "replay:" + replay_id + ":" + exp_unix))>
//
// The literal "replay:" prefix is a domain separator so the same secret
// cannot be reused to forge admin session cookies.
//
// See docs/specs/foundations/08-leaderboard-replay.md#streaming-replay-engine
// and docs/specs/client/11-replay-viewer.md#auth-flow.
package replayauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

// TTL is the lifetime of a freshly-minted ticket. Long enough that any
// reasonable single replay watch finishes in time; short enough that a
// leaked ticket has bounded value.
const TTL = 30 * time.Minute

// ErrInvalid is returned for malformed, mismatched, or HMAC-failed
// tickets. ErrExpired is returned when the exp has passed.
var (
	ErrInvalid = errors.New("replayauth: invalid ticket")
	ErrExpired = errors.New("replayauth: ticket expired")
)

// Sign mints a ticket binding replayID to exp. Caller picks exp; use
// time.Now().Add(TTL) for a default-aged ticket.
func Sign(secret []byte, replayID string, exp time.Time) string {
	expStr := strconv.FormatInt(exp.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("replay:" + replayID + ":" + expStr))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return replayID + "." + expStr + "." + sig
}

// Verify validates a ticket against secret and ensures it binds the
// expected replayID. The current time is compared against the ticket's
// embedded exp using `now`. Returns nil on success, ErrInvalid on shape
// / HMAC failure, or ErrExpired on a stale ticket.
//
// The replayID-binding check is load-bearing: without it a ticket
// minted for one replay could be used to view a different replay.
func Verify(secret []byte, ticket, expectedReplayID string, now time.Time) error {
	if ticket == "" || expectedReplayID == "" {
		return ErrInvalid
	}
	dot1 := strings.IndexByte(ticket, '.')
	if dot1 <= 0 || dot1 == len(ticket)-1 {
		return ErrInvalid
	}
	rest := ticket[dot1+1:]
	dot2 := strings.IndexByte(rest, '.')
	if dot2 <= 0 || dot2 == len(rest)-1 {
		return ErrInvalid
	}
	replayID := ticket[:dot1]
	expStr := rest[:dot2]
	sig := rest[dot2+1:]
	if subtle.ConstantTimeCompare([]byte(replayID), []byte(expectedReplayID)) != 1 {
		return ErrInvalid
	}
	expUnix, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return ErrInvalid
	}
	if now.Unix() >= expUnix {
		return ErrExpired
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("replay:" + replayID + ":" + expStr))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return ErrInvalid
	}
	return nil
}
