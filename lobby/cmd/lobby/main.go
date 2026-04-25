// Package main is the lobby service entry point.
//
// Phase 1 (#21) hosts identity HTTP serving. Phase 2 (#22) adds the public
// leaderboard webapp + replay download. Phase 3 (#23) adds the spectator
// selector and (later) the spectator viewer page. Later phases add source
// serving, admin panel, and the FIFO queue.
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
	"github.com/greyhats/defcon-game/lobby/internal/leaderboard"
	"github.com/greyhats/defcon-game/lobby/internal/livegames"
	"github.com/greyhats/defcon-game/lobby/web"
	"github.com/greyhats/defcon-game/pkg/identity"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	addr := envDefault("LISTEN_ADDR", ":3001")
	identityDBPath := envDefault("IDENTITY_DB", "/data/identity.db")
	gameDBPath := envDefault("GAME_DB", "/data/game.db")
	gameServerURL := envDefault("GAME_SERVER_URL", "http://game-server:8080")
	sourceTarball := envDefault("SOURCE_TARBALL", "/app/source.tar.gz")
	adminUser := os.Getenv("ADMIN_USER")
	adminPassHash := []byte(os.Getenv("ADMIN_PASS_HASH"))

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
	mux.HandleFunc("/api/replay/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/replay/")
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
	})
	mux.HandleFunc("/spectate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(web.SpectateHTML)
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
		default:
			http.NotFound(w, r)
		}
	})))

	// /source — landing page that points at the tarball. /source.tar.gz
	// streams the file built at image-build time by the strip stage in
	// lobby/Dockerfile. Players download once and browse the tree
	// locally; no per-file HTTP traversal, no index.html collisions
	// with the stripped client/index.html, no surprise SPA rendering.
	mux.HandleFunc("/source", func(w http.ResponseWriter, r *http.Request) {
		// Allow GET and HEAD — http.ServeFile (used by /source.tar.gz)
		// supports HEAD natively, and rejecting it from /source for
		// "consistency" was making Chrome's download manager 405 on its
		// pre-flight HEAD and stall the .tar.gz download at 99%. Match
		// the same policy here so the entire /source* surface is
		// browser-friendly.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(web.SourceHTML)
	})
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
		log.Printf("lobby: listening on %s (identity_db=%s game_db=%s game_server=%s source_tarball=%s admin=%s)", addr, identityDBPath, gameDBPath, gameServerURL, sourceTarball, adminEnabled)
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
