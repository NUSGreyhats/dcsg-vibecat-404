// Package identity is a STUB shipped with the published source so that
// server/ compiles and runs locally for puzzle research. The real
// pkg/identity is not published — see
// docs/specs/foundations/09-ci-cd-deploy.md#what-is-not-published.
//
// This stub accepts every player_id as registered with DisplayName equal
// to the player_id. It performs no authentication, no rate limiting, and
// no persistence. Do not deploy.
package identity

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("identity: player not found")

type Player struct {
	DisplayName string
}

type Service struct{ db *sql.DB }

func NewService(db *sql.DB) *Service { return &Service{db: db} }

func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/register", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			DisplayName string `json:"display_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DisplayName == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "display_name required"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"player_id": req.DisplayName})
	})
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username required"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"player_id":    req.Username,
			"display_name": req.Username,
		})
	})
	mux.HandleFunc("/api/verify", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PlayerID string `json:"player_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PlayerID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "player_id required"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"display_name": req.PlayerID})
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func OpenDB(_ string) (*sql.DB, error) {
	log.Println("identity: STUB build — every player_id is accepted, no auth")
	return sql.Open("sqlite", ":memory:")
}

func Lookup(_ *sql.DB, playerID string) (*Player, error) {
	return &Player{DisplayName: playerID}, nil
}
