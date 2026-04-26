// Package main is the lobby service entry point.
//
// Phase 1 (#21) hosts identity HTTP serving. Phase 2 (#22) adds the public
// leaderboard webapp + replay download. Phase 3 (#23) adds the spectator
// selector and viewer page. Phase 4 (#24) adds source serving, the
// admin panel, and moves replay download behind admin auth so input
// logs aren't publicly mineable for the intentional bugs. Phase 5 (#25)
// adds the FIFO queue + slot handoff via /data/allowlist.json, gated on
// QUEUE_ENABLED so dev deploys (AUTH_MODE=identity) keep the queue
// dormant.
//
// Spec: docs/specs/foundations/04-lobby.md.
package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/greyhats/defcon-game/lobby/internal/admin"
	"github.com/greyhats/defcon-game/lobby/internal/advancer"
	"github.com/greyhats/defcon-game/lobby/internal/leaderboard"
	"github.com/greyhats/defcon-game/lobby/internal/livegames"
	"github.com/greyhats/defcon-game/lobby/internal/queue"
	"github.com/greyhats/defcon-game/lobby/web"
	"github.com/greyhats/defcon-game/pkg/identity"
)

// avgTurnMs is the rough turn duration used to compute est_wait_ms for
// queued players. Spec sets the per-turn TTL at 5 minutes, but most
// runs end well before that — 3 min is a reasonable midpoint and is
// only ever surfaced to a waiting player as informational.
const avgTurnMs = 180_000

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	addr := envDefault("LISTEN_ADDR", ":3001")
	identityDBPath := envDefault("IDENTITY_DB", "/data/identity.db")
	gameDBPath := envDefault("GAME_DB", "/data/game.db")
	lobbyDBPath := envDefault("LOBBY_DB", "/data/lobby.db")
	allowlistPath := envDefault("ALLOWLIST_PATH", "/data/allowlist.json")
	gameServerURL := envDefault("GAME_SERVER_URL", "http://game-server:8080")
	sourceTarball := envDefault("SOURCE_TARBALL", "/app/source.tar.gz")
	adminUser := os.Getenv("ADMIN_USER")
	adminPassHash := []byte(os.Getenv("ADMIN_PASS_HASH"))
	queueEnabled := strings.EqualFold(os.Getenv("QUEUE_ENABLED"), "true")

	identityDB, err := identity.OpenDB(identityDBPath)
	if err != nil {
		log.Fatalf("open identity db: %v", err)
	}
	defer identityDB.Close()

	gameDB, err := leaderboard.Open(gameDBPath)
	if err != nil {
		log.Fatalf("open game db: %v", err)
	}
	defer gameDB.Close()

	// Ensure admin-owned schema exists (flagged_runs). Idempotent.
	if err := admin.EnsureSchema(gameDB); err != nil {
		log.Fatalf("admin schema: %v", err)
	}

	auth := &admin.Auth{User: adminUser, PassHash: adminPassHash}

	identitySvc := identity.NewService(identityDB)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Live-games poller — pulls /internal/live-games from the game server
	// every 2 s and caches the response. Backs /api/live and the
	// /spectate selector page.
	livePoller := livegames.New(gameServerURL)
	livePoller.Start(ctx)

	// Queue + slot advancer (Phase 5). Both are kept off when
	// QUEUE_ENABLED isn't true so that dev deploys with
	// AUTH_MODE=identity don't spuriously rewrite allowlist.json (the
	// game server would ignore it, but the disk churn is wasted) and
	// public /api/queue/* returns 503 instead of accepting joins that
	// will never advance.
	var (
		queueDB *sql.DB
		adv     *advancer.Advancer
	)
	if queueEnabled {
		queueDB, err = queue.Open(lobbyDBPath)
		if err != nil {
			log.Fatalf("open lobby db: %v", err)
		}
		defer queueDB.Close()
		adv = advancer.New(queueDB, livePoller, gameServerURL, allowlistPath)
		adv.Start(ctx)
	}

	mux := http.NewServeMux()
	identitySvc.Register(mux)
	mux.HandleFunc("/register", serveRegisterPage)
	mux.HandleFunc("/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(web.LeaderboardHTML)
	})
	mux.HandleFunc("/api/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		limit := 10
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}
		entries, err := leaderboard.TopRuns(gameDB, limit)
		if err != nil {
			log.Printf("leaderboard: top: %v", err)
			http.Error(w, "leaderboard unavailable", http.StatusInternalServerError)
			return
		}
		if entries == nil {
			entries = []leaderboard.Entry{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": entries})
	})
	mux.HandleFunc("/spectate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(web.SpectateHTML)
	})
	// /api/queue/* — Phase 5 FIFO queue. All routes return 503 if
	// QUEUE_ENABLED isn't set (dev deploys, where the game server
	// accepts everyone via AUTH_MODE=identity).
	mux.HandleFunc("/api/queue/join", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !queueEnabled {
			http.Error(w, "queue disabled", http.StatusServiceUnavailable)
			return
		}
		queueJoinHandler(w, r, queueDB, identityDB, adv)
	})
	mux.HandleFunc("/api/queue/status/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !queueEnabled {
			http.Error(w, "queue disabled", http.StatusServiceUnavailable)
			return
		}
		queueStatusHandler(w, r, queueDB, adv)
	})
	mux.HandleFunc("/api/queue/", func(w http.ResponseWriter, r *http.Request) {
		// Catch-all for /api/queue/:player_id (DELETE). Other prefixes
		// (/api/queue/join, /api/queue/status/...) are handled above.
		if !queueEnabled {
			http.Error(w, "queue disabled", http.StatusServiceUnavailable)
			return
		}
		switch r.Method {
		case http.MethodDelete:
			queueLeaveHandler(w, r, queueDB, identityDB)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/live", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Verbatim cache — the game-server's spectator.Summary JSON shape
		// is the contract here. If the cache hasn't populated yet (or the
		// game server is down), this is the empty slice.
		entries := livePoller.Snapshot()
		if entries == nil {
			entries = []livegames.Entry{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entries)
	})
	// /admin/* — Basic-auth-gated moderation panel. Auth.Wrap returns
	// 503 "admin disabled" if ADMIN_USER is empty, so a deployment that
	// hasn't configured creds can't accidentally serve flag/delete.
	mux.Handle("/admin/", auth.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && (r.URL.Path == "/admin/" || r.URL.Path == "/admin"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(web.AdminHTML)
		case r.Method == http.MethodGet && r.URL.Path == "/admin/leaderboard.json":
			entries, err := admin.AllRuns(gameDB, 200)
			if err != nil {
				log.Printf("admin: list runs: %v", err)
				http.Error(w, "list failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(entries)
		case r.Method == http.MethodPost && r.URL.Path == "/admin/leaderboard/flag":
			adminFlagHandler(w, r, gameDB, true)
		case r.Method == http.MethodPost && r.URL.Path == "/admin/leaderboard/unflag":
			adminFlagHandler(w, r, gameDB, false)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/admin/replay/"):
			id := strings.TrimPrefix(r.URL.Path, "/admin/replay/")
			if id == "" || strings.Contains(id, "/") {
				http.Error(w, "invalid replay id", http.StatusBadRequest)
				return
			}
			replay, err := leaderboard.GetReplay(gameDB, id)
			if err != nil {
				if errors.Is(err, leaderboard.ErrNotFound) {
					http.Error(w, "not found", http.StatusNotFound)
					return
				}
				if errors.Is(err, leaderboard.ErrChecksumMismatch) {
					log.Printf("leaderboard: replay %s: checksum mismatch", id)
					http.Error(w, "replay integrity check failed", http.StatusInternalServerError)
					return
				}
				log.Printf("leaderboard: replay %s: %v", id, err)
				http.Error(w, "replay unavailable", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(replay)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/admin/leaderboard/"):
			id := strings.TrimPrefix(r.URL.Path, "/admin/leaderboard/")
			if id == "" || strings.Contains(id, "/") {
				http.Error(w, "invalid replay id", http.StatusBadRequest)
				return
			}
			if err := admin.HardDelete(gameDB, id); err != nil {
				log.Printf("admin: delete %s: %v", id, err)
				http.Error(w, "delete failed", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/admin/winners.csv":
			adminWinnersCSV(w, gameDB, identityDB)
		case r.Method == http.MethodGet && r.URL.Path == "/admin/queue.json":
			adminQueueJSON(w, queueEnabled, queueDB, adv)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/admin/queue/"):
			id := strings.TrimPrefix(r.URL.Path, "/admin/queue/")
			adminQueueKick(w, queueEnabled, queueDB, id)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/admin/slot/free/"):
			id := strings.TrimPrefix(r.URL.Path, "/admin/slot/free/")
			adminSlotFree(w, queueEnabled, adv, id)
		default:
			http.NotFound(w, r)
		}
	})))

	// /source.tar.gz streams the stripped tarball baked into the image
	// at build time by the strip stage in lobby/Dockerfile (linked from
	// the nav bar of /register, /leaderboard, /spectate, /admin/, so no
	// dedicated landing page). Players download once and browse the
	// tree locally; no per-file HTTP traversal, no index.html
	// collisions with the stripped client/index.html, no surprise SPA
	// rendering. SOURCE_TARBALL only needs to be set for ad-hoc
	// testing — the default points at the baked path.
	mux.HandleFunc("/source.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="source.tar.gz"`)
		http.ServeFile(w, r, sourceTarball)
	})
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		adminEnabled := "off"
		if adminUser != "" && len(adminPassHash) > 0 {
			adminEnabled = "on (user=" + adminUser + ")"
		}
		queueState := "off"
		if queueEnabled {
			queueState = "on (db=" + lobbyDBPath + " allowlist=" + allowlistPath + ")"
		}
		log.Printf("lobby: listening on %s (identity_db=%s game_db=%s game_server=%s source_tarball=%s admin=%s queue=%s)", addr, identityDBPath, gameDBPath, gameServerURL, sourceTarball, adminEnabled, queueState)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("lobby: shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}

func envDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// adminFlagHandler is the shared body for POST /admin/leaderboard/{flag,unflag}.
// Body is JSON `{"replay_id": "...", "reason": "..."}`. Reason is ignored
// for unflag.
func adminFlagHandler(w http.ResponseWriter, r *http.Request, db *sql.DB, flag bool) {
	var body struct {
		ReplayID string `json:"replay_id"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if body.ReplayID == "" {
		http.Error(w, "replay_id required", http.StatusBadRequest)
		return
	}
	var err error
	if flag {
		err = admin.Flag(db, body.ReplayID, body.Reason)
	} else {
		err = admin.Unflag(db, body.ReplayID)
	}
	if err != nil {
		log.Printf("admin: flag %s: %v", body.ReplayID, err)
		http.Error(w, "flag op failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// queueJoinHandler is POST /api/queue/join. Bcrypt-verifies the password
// against identity.db, inserts into queue_entries on success, and
// returns the new position + estimated wait. Existing queued players
// get 409 — they must DELETE first if they want to re-join (and lose
// their position).
//
// If the player is already admitted (popped from the queue and waiting
// for game-server slot), we return 200 with position=0 so the client
// can short-circuit straight to the play page rather than getting a
// confusing "already queued" error.
func queueJoinHandler(w http.ResponseWriter, r *http.Request, queueDB, identityDB *sql.DB, adv *advancer.Advancer) {
	var body struct {
		PlayerID string `json:"player_id"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if body.PlayerID == "" || body.Password == "" {
		http.Error(w, "player_id and password required", http.StatusBadRequest)
		return
	}
	p, err := identity.Lookup(identityDB, body.PlayerID)
	if err != nil {
		if errors.Is(err, identity.ErrNotFound) {
			// Dummy compare to equalize timing with the bcrypt hit path.
			_ = identity.DummyCompare(body.Password)
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		log.Printf("queue/join: lookup %s: %v", body.PlayerID, err)
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if err := identity.VerifyPassword(p.PasswordHash, body.Password); err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if adv != nil && adv.Admitted(body.PlayerID) {
		writeQueueStatus(w, 0, true)
		return
	}

	if err := queue.Join(queueDB, body.PlayerID, time.Now().UnixMilli()); err != nil {
		if errors.Is(err, queue.ErrAlreadyQueued) {
			http.Error(w, "already queued", http.StatusConflict)
			return
		}
		log.Printf("queue/join: insert %s: %v", body.PlayerID, err)
		http.Error(w, "queue write failed", http.StatusInternalServerError)
		return
	}
	pos, err := queue.Position(queueDB, body.PlayerID)
	if err != nil {
		log.Printf("queue/join: position %s: %v", body.PlayerID, err)
		http.Error(w, "position lookup failed", http.StatusInternalServerError)
		return
	}
	writeQueueStatus(w, pos, false)
}

// queueStatusHandler is GET /api/queue/status/:player_id. Returns
// position (0 = admitted), est_wait_ms, and admitted flag. No auth —
// the player_id alone is sufficient (it's hex-32 and unguessable, and
// status leaks nothing the WS auth wouldn't already).
func queueStatusHandler(w http.ResponseWriter, r *http.Request, queueDB *sql.DB, adv *advancer.Advancer) {
	id := strings.TrimPrefix(r.URL.Path, "/api/queue/status/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "invalid player_id", http.StatusBadRequest)
		return
	}
	if adv != nil && adv.Admitted(id) {
		writeQueueStatus(w, 0, true)
		return
	}
	pos, err := queue.Position(queueDB, id)
	if err != nil {
		log.Printf("queue/status: position %s: %v", id, err)
		http.Error(w, "position lookup failed", http.StatusInternalServerError)
		return
	}
	if pos == 0 {
		http.Error(w, "not queued", http.StatusNotFound)
		return
	}
	writeQueueStatus(w, pos, false)
}

// queueLeaveHandler is DELETE /api/queue/:player_id. Body is JSON
// `{"password": "..."}`. Self-removal only — there is no admin override
// here; admins use /admin/queue.
func queueLeaveHandler(w http.ResponseWriter, r *http.Request, queueDB, identityDB *sql.DB) {
	id := strings.TrimPrefix(r.URL.Path, "/api/queue/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "invalid player_id", http.StatusBadRequest)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if body.Password == "" {
		http.Error(w, "password required", http.StatusBadRequest)
		return
	}
	p, err := identity.Lookup(identityDB, id)
	if err != nil {
		if errors.Is(err, identity.ErrNotFound) {
			_ = identity.DummyCompare(body.Password)
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		log.Printf("queue/leave: lookup %s: %v", id, err)
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if err := identity.VerifyPassword(p.PasswordHash, body.Password); err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if err := queue.Leave(queueDB, id); err != nil {
		if errors.Is(err, queue.ErrNotQueued) {
			http.Error(w, "not queued", http.StatusNotFound)
			return
		}
		log.Printf("queue/leave: delete %s: %v", id, err)
		http.Error(w, "leave failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeQueueStatus serialises the public {position, est_wait_ms, admitted}
// shape used by both /api/queue/join and /api/queue/status.
func writeQueueStatus(w http.ResponseWriter, position int, admitted bool) {
	body := struct {
		Position  int   `json:"position"`
		EstWaitMs int64 `json:"est_wait_ms"`
		Admitted  bool  `json:"admitted"`
	}{
		Position:  position,
		EstWaitMs: int64(position) * avgTurnMs,
		Admitted:  admitted,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// adminQueueJSON returns the current queue + admittedSet for the admin
// panel. queue is sorted oldest-first (= front of line first).
func adminQueueJSON(w http.ResponseWriter, queueEnabled bool, queueDB *sql.DB, adv *advancer.Advancer) {
	if !queueEnabled {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"enabled": false,
		})
		return
	}
	entries, err := queue.List(queueDB, 200)
	if err != nil {
		log.Printf("admin: queue list: %v", err)
		http.Error(w, "queue list failed", http.StatusInternalServerError)
		return
	}
	queueOut := make([]map[string]any, 0, len(entries))
	for i, e := range entries {
		queueOut = append(queueOut, map[string]any{
			"player_id": e.PlayerID,
			"position":  i + 1,
			"joined_at": e.JoinedAt,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"enabled":  true,
		"queue":    queueOut,
		"admitted": adv.AdmittedSnapshot(),
	})
}

// adminQueueKick removes a player from the queue. Returns 404 if they
// weren't in the queue (so a stale UI click doesn't 500).
func adminQueueKick(w http.ResponseWriter, queueEnabled bool, queueDB *sql.DB, playerID string) {
	if !queueEnabled {
		http.Error(w, "queue disabled", http.StatusServiceUnavailable)
		return
	}
	if playerID == "" || strings.Contains(playerID, "/") {
		http.Error(w, "invalid player_id", http.StatusBadRequest)
		return
	}
	if err := queue.Leave(queueDB, playerID); err != nil {
		if errors.Is(err, queue.ErrNotQueued) {
			http.Error(w, "not queued", http.StatusNotFound)
			return
		}
		log.Printf("admin: queue kick %s: %v", playerID, err)
		http.Error(w, "kick failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminSlotFree force-evicts an admitted player so the next tick can
// admit the next queue head. Returns 404 if the player isn't admitted.
func adminSlotFree(w http.ResponseWriter, queueEnabled bool, adv *advancer.Advancer, playerID string) {
	if !queueEnabled {
		http.Error(w, "queue disabled", http.StatusServiceUnavailable)
		return
	}
	if playerID == "" || strings.Contains(playerID, "/") {
		http.Error(w, "invalid player_id", http.StatusBadRequest)
		return
	}
	if !adv.ForceFree(playerID) {
		http.Error(w, "not admitted", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminWinnersCSV streams the non-flagged top runs joined with player
// contact info as a CSV download.
func adminWinnersCSV(w http.ResponseWriter, gameDB, identityDB *sql.DB) {
	winners, err := admin.TopWinners(gameDB, identityDB, 100)
	if err != nil {
		log.Printf("admin: winners: %v", err)
		http.Error(w, "export failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="winners.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"replay_id", "player_id", "display_name", "contact", "total_ms", "submitted_at"})
	for _, win := range winners {
		_ = cw.Write([]string{
			win.ReplayID,
			win.PlayerID,
			win.DisplayName,
			win.Contact,
			strconv.FormatInt(win.TotalMs, 10),
			win.SubmittedAt,
		})
	}
	cw.Flush()
}
