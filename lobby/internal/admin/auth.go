// Package admin owns the lobby's /admin/* surface — auth-protected
// endpoints for moderating the public leaderboard and exporting winner
// contact info. Spec: docs/specs/foundations/04-lobby.md#admin-panel.
//
// Auth strategy is HTTP Basic with credentials sourced from the
// ADMIN_USER and ADMIN_PASS_HASH env vars. Empty ADMIN_USER disables
// the panel entirely (useful for tests / dev where no creds are set).
package admin

import (
	"crypto/subtle"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

// dummyHash is a valid bcrypt hash used when the username is wrong, so
// the response time on bad-username matches bad-password (~one bcrypt
// compare). Otherwise an attacker can probe usernames by timing the
// response — a known username takes ~70 ms (bcrypt cost 10), an
// unknown one returns in microseconds. The plaintext doesn't matter;
// the goal is just to make CompareHashAndPassword do the same work.
// (This is the well-known test vector for "password" at cost 10.)
var dummyHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

// Auth is a Basic-auth gate. User is the expected admin username;
// PassHash is the bcrypt hash of the expected password. Both must be
// non-empty for the gate to authorize anything — an unset User makes
// every request 503 with "admin disabled".
type Auth struct {
	User     string
	PassHash []byte
}

// Wrap returns a handler that enforces Basic auth before delegating
// to next. Failed auth responds with 401 + WWW-Authenticate so curl
// and browsers prompt for credentials.
func (a *Auth) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.User == "" || len(a.PassHash) == 0 {
			http.Error(w, "admin disabled", http.StatusServiceUnavailable)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok {
			a.challenge(w)
			return
		}
		// Constant-time compare on the username, then ALWAYS run a
		// bcrypt compare — against the real hash if the username
		// matched, against a dummy hash if it didn't. Without the
		// dummy compare, "wrong username" returns in microseconds
		// while "right username, wrong password" takes ~one bcrypt
		// (~70 ms at cost 10), letting an attacker enumerate
		// usernames purely by timing. We fold both checks into a
		// single authorize bool so an early return can't reintroduce
		// the side channel.
		userOK := subtle.ConstantTimeCompare([]byte(user), []byte(a.User)) == 1
		hash := a.PassHash
		if !userOK {
			hash = dummyHash
		}
		passOK := bcrypt.CompareHashAndPassword(hash, []byte(pass)) == nil
		if !userOK || !passOK {
			a.challenge(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Auth) challenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="lobby admin"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}
