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
	mux.HandleFunc("/api/live", func(w http.ResponseWriter, _ *http.Request) {
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
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("lobby: listening on %s (identity_db=%s game_db=%s game_server=%s)", addr, identityDBPath, gameDBPath, gameServerURL)
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
