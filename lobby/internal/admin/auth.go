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
		// Constant-time compare on the username so a probe can't
		// time-distinguish "user is admin" from "user is wrong".
		if subtle.ConstantTimeCompare([]byte(user), []byte(a.User)) != 1 {
			a.challenge(w)
			return
		}
		if err := bcrypt.CompareHashAndPassword(a.PassHash, []byte(pass)); err != nil {
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
