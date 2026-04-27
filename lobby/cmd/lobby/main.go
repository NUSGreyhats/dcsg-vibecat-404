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
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/greyhats/defcon-game/lobby/internal/admin"
	"github.com/greyhats/defcon-game/lobby/internal/adminauth"
	"github.com/greyhats/defcon-game/lobby/internal/advancer"
	"github.com/greyhats/defcon-game/lobby/internal/leaderboard"
	"github.com/greyhats/defcon-game/lobby/internal/livegames"
	"github.com/greyhats/defcon-game/lobby/internal/queue"
	"github.com/greyhats/defcon-game/lobby/internal/settings"
	"github.com/greyhats/defcon-game/lobby/web"
	"github.com/greyhats/defcon-game/pkg/identity"
	"github.com/greyhats/defcon-game/pkg/netutil"
	"github.com/greyhats/defcon-game/pkg/replayauth"
)

// avgTurnMs is the rough turn duration used to compute est_wait_ms for
// queued players. Spec sets the per-turn TTL at 5 minutes, but most
// runs end well before that — 3 min is a reasonable midpoint and is
// only ever surfaced to a waiting player as informational.
const avgTurnMs = 180_000

// maxQueueBodyBytes caps incoming JSON for /api/queue/{join,leave}.
// Mirrors the limit pkg/identity uses for /api/login etc. — the
// payloads are tiny ({player_id, password}); 4 KiB is well over real
// payloads but stops a long body from chewing memory.
const maxQueueBodyBytes = 4 * 1024

// maxAdminBodyBytes caps incoming JSON for /admin/* mutating endpoints
// (e.g. flag/unflag). Bodies carry a replay_id (~32 hex) plus an
// optional reason; 2 KiB is well over realistic payloads.
const maxAdminBodyBytes = 2 * 1024

// maxFlagReasonBytes caps the stored moderation reason to keep one
// large admin POST from inflating game.db. Plenty for a one-line note.
const maxFlagReasonBytes = 512

// playerIDPattern is the canonical 32-hex-character shape produced by
// identity.NewPlayerID. Queue endpoints reject anything else early so
// malformed IDs don't reach the DB.
var playerIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func validPlayerID(s string) bool {
	return playerIDPattern.MatchString(s)
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	addr := envDefault("LISTEN_ADDR", ":3001")
	identityDBPath := envDefault("IDENTITY_DB", "/data/identity.db")
	gameDBPath := envDefault("GAME_DB", "/data/game.db")
	lobbyDBPath := envDefault("LOBBY_DB", "/data/lobby.db")
	allowlistPath := envDefault("ALLOWLIST_PATH", "/data/allowlist.json")
	gameServerURL := envDefault("GAME_SERVER_URL", "http://game-server:8080")
	sourceTarball := envDefault("SOURCE_TARBALL", "/app/source.tar.gz")
	telegramBotToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	telegramChatID := os.Getenv("TELEGRAM_ADMIN_CHAT_ID")
	adminSessionSecret := []byte(os.Getenv("ADMIN_SESSION_SECRET"))
	queueEnabled := strings.EqualFold(os.Getenv("QUEUE_ENABLED"), "true")
	// INTERNAL_SECRET — shared bearer for /internal/* requests to the
	// game server. Threaded into the live-games poller and the
	// slot-status fetcher in the advancer. Empty disables the header
	// (matches game server's default — relies on private-peer gating
	// only). Operator should set this in any prod deploy.
	internalSecret := os.Getenv("INTERNAL_SECRET")
	if internalSecret == "" {
		log.Printf("WARN: INTERNAL_SECRET unset — /internal/* polls won't carry an auth header.")
	}
	// BEHIND_TLS=1 tells adminauth to issue Secure cookies regardless of
	// what the request looks like. Set in any prod deploy where TLS is
	// terminated upstream (Caddy on the host, Cloudflare, etc.) so a
	// proxy that omits X-Forwarded-Proto can't silently downgrade the
	// admin_session cookie to non-Secure.
	behindTLS := strings.EqualFold(os.Getenv("BEHIND_TLS"), "true") ||
		os.Getenv("BEHIND_TLS") == "1"

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

	// lobby.db is opened unconditionally — both the queue (when
	// QUEUE_ENABLED=true) and adminauth (always, when admin creds are
	// set) live there. Opening with no consumers is cheap.
	lobbyDB, err := queue.Open(lobbyDBPath)
	if err != nil {
		log.Fatalf("open lobby db: %v", err)
	}
	defer lobbyDB.Close()
	if err := adminauth.EnsureSchema(lobbyDB); err != nil {
		log.Fatalf("adminauth schema: %v", err)
	}
	if err := settings.EnsureSchema(lobbyDB); err != nil {
		log.Fatalf("settings schema: %v", err)
	}

	authSvc, err := adminauth.New(lobbyDB, adminauth.Config{
		BotToken:           telegramBotToken,
		ChatID:             telegramChatID,
		SessionSecret:      adminSessionSecret,
		ForceSecureCookies: behindTLS,
	})
	if err != nil {
		log.Fatalf("adminauth: %v", err)
	}

	identitySvc := identity.NewService(identityDB)

	// Per-IP rate limit on /api/queue/{join,leave}. 1 s mirrors
	// /api/login and is intentionally tight: both queue endpoints
	// run bcrypt password verification, which is exactly the CPU
	// load we don't want unauthenticated traffic to be able to
	// trigger at line rate. See bug #4 in issue 71.
	queueLimit := identity.NewRateLimiter(1 * time.Second)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Live-games poller — pulls /internal/live-games from the game server
	// every 2 s and caches the response. Backs /api/live and the
	// /spectate selector page.
	livePoller := livegames.New(gameServerURL, internalSecret)
	livePoller.Start(ctx)

	// Queue + slot advancer (Phase 5). Both are kept off when
	// QUEUE_ENABLED isn't true so that dev deploys with
	// AUTH_MODE=identity don't spuriously rewrite allowlist.json (the
	// game server would ignore it, but the disk churn is wasted) and
	// public /api/queue/* returns 503 instead of accepting joins that
	// will never advance. The lobby.db handle (lobbyDB) is always
	// opened above; the advancer just borrows it.
	var (
		queueDB *sql.DB
		adv     *advancer.Advancer
	)
	if queueEnabled {
		queueDB = lobbyDB
		// overrideMax is consulted on every advancer tick, so admin-set
		// changes take effect within ~2 s without a restart. lobby.db
		// always has a row (EnsureSchema seeds DefaultMaxConcurrent),
		// so ok=true is the normal case. ok=false only when the DB
		// read errors — the advancer then falls through to
		// /internal/slot-status as defense-in-depth.
		overrideMax := func() (int, bool) {
			n, err := settings.GetMaxConcurrent(lobbyDB)
			if err != nil {
				log.Printf("settings: get max concurrent: %v", err)
				return 0, false
			}
			return n, true
		}
		adv = advancer.New(queueDB, livePoller, gameServerURL, internalSecret, allowlistPath, overrideMax)
		adv.Start(ctx)
	}

	mux := http.NewServeMux()
	identitySvc.Register(mux)
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
		dedupe := true
		if v := r.URL.Query().Get("dedupe"); v == "0" || v == "false" {
			dedupe = false
		}
		entries, err := leaderboard.TopRuns(gameDB, limit, dedupe)
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
		queueJoinHandler(w, r, queueDB, identityDB, adv, queueLimit)
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
			queueLeaveHandler(w, r, queueDB, identityDB, queueLimit)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/live", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Strip player_id before serving — it's the WS bearer credential
		// in token mode and used to be observable on this endpoint and on
		// every spectator URL (issue 71 #1). RunID is the public-facing
		// per-run handle clients build /spectate links from.
		entries := livePoller.Snapshot()
		out := make([]livegames.PublicEntry, 0, len(entries))
		for _, e := range entries {
			out = append(out, e.Public())
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	// /admin/login + /admin/verify + /admin/logout are public — they
	// have to be reachable to bootstrap a session. They internally
	// 503 when admin auth isn't configured.
	mux.HandleFunc("/admin/login", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			authSvc.ServeLoginPage(w, r, web.AdminLoginHTML)
		case http.MethodPost:
			authSvc.HandleLogin(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/admin/verify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		authSvc.HandleVerify(w, r)
	})
	mux.HandleFunc("/admin/logout", func(w http.ResponseWriter, r *http.Request) {
		authSvc.HandleLogout(w, r)
	})

	// /admin/* — Telegram-OTP-gated moderation panel. authSvc.Wrap
	// returns 503 "admin disabled" when TELEGRAM_BOT_TOKEN /
	// TELEGRAM_ADMIN_CHAT_ID / ADMIN_SESSION_SECRET aren't all set,
	// so a deployment that hasn't configured the bot can't
	// accidentally serve flag/delete.
	mux.Handle("/admin/", authSvc.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /admin/login, /admin/verify, /admin/logout are intercepted
		// by the more-specific HandleFunc registrations above, so
		// they never reach this gated branch.
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
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/meta") && strings.HasPrefix(r.URL.Path, "/admin/replay/"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/admin/replay/"), "/meta")
			adminReplayMetaHandler(w, gameDB, id)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/ticket") && strings.HasPrefix(r.URL.Path, "/admin/replay/"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/admin/replay/"), "/ticket")
			adminReplayTicketHandler(w, gameDB, id, adminSessionSecret)
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
		case r.Method == http.MethodGet && r.URL.Path == "/admin/settings.json":
			adminSettingsJSON(w, lobbyDB)
		case r.Method == http.MethodPost && r.URL.Path == "/admin/settings/max-concurrent":
			adminSetMaxConcurrent(w, r, lobbyDB)
		default:
			http.NotFound(w, r)
		}
	})))

	// /source.tar.gz streams the stripped tarball baked into the image
	// at build time by the strip stage in lobby/Dockerfile (linked from
	// the nav bar of /leaderboard, /spectate, /admin/, so no
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
		Addr:    addr,
		Handler: mux,
		// Slowloris hardening — without these a slow client can hold a
		// handler goroutine open indefinitely. Lobby has no WebSocket
		// endpoints; all routes are short-lived JSON / HTML so the
		// 30 s caps are well above legit traffic.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		adminEnabled := "off"
		if authSvc.Enabled() {
			adminEnabled = "on (telegram chat=" + telegramChatID + ")"
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
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAdminBodyBytes)).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if body.ReplayID == "" {
		http.Error(w, "replay_id required", http.StatusBadRequest)
		return
	}
	if len(body.Reason) > maxFlagReasonBytes {
		// Byte-slice the cap, then walk back to a rune boundary so we
		// never store invalid UTF-8 — naive [:n] can split a multi-byte
		// rune (any non-ASCII char) and break SQLite text handling and
		// the admin UI's render. utf8.ValidString recovers in at most
		// 3 bytes (max rune length is 4 → cap could land inside a 4-
		// byte rune that started at cap-3).
		s := body.Reason[:maxFlagReasonBytes]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
		body.Reason = s
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
func queueJoinHandler(w http.ResponseWriter, r *http.Request, queueDB, identityDB *sql.DB, adv *advancer.Advancer, limiter *identity.RateLimiter) {
	// Per-IP rate limit BEFORE any decode/lookup/bcrypt. This is a
	// public endpoint that runs bcrypt on every accepted request, so
	// without throttling a single attacker can pin a CPU core or
	// online-brute-force a known player_id. We also skip the dummy
	// bcrypt path on rate-limit so abusive traffic does no work.
	if !limiter.Allow(netutil.ClientIP(r)) {
		http.Error(w, "slow down", http.StatusTooManyRequests)
		return
	}
	var body struct {
		PlayerID string `json:"player_id"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxQueueBodyBytes)).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if body.PlayerID == "" || body.Password == "" {
		http.Error(w, "player_id and password required", http.StatusBadRequest)
		return
	}
	if !validPlayerID(body.PlayerID) {
		// Wrong format gets the same 401 as a real auth failure to
		// keep the surface uniform; brute-force probing of player_id
		// shouldn't get a free format-validity oracle.
		_ = identity.DummyCompare(body.Password)
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
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
	// pos==0 here means Position couldn't find the row — possible if
	// the advancer popped the just-inserted head between the INSERT
	// and the Position SELECT. Re-check Admitted so the caller sees
	// {position:0, admitted:true} instead of a confusing
	// {position:0, admitted:false} that would render as "0th in line".
	if pos == 0 && adv != nil && adv.Admitted(body.PlayerID) {
		writeQueueStatus(w, 0, true)
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
	if !validPlayerID(id) {
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
func queueLeaveHandler(w http.ResponseWriter, r *http.Request, queueDB, identityDB *sql.DB, limiter *identity.RateLimiter) {
	// Per-IP rate limit BEFORE bcrypt — same reasoning as queueJoinHandler.
	if !limiter.Allow(netutil.ClientIP(r)) {
		http.Error(w, "slow down", http.StatusTooManyRequests)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/queue/")
	if !validPlayerID(id) {
		http.Error(w, "invalid player_id", http.StatusBadRequest)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxQueueBodyBytes)).Decode(&body); err != nil {
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
//
// est_wait_ms estimates the time the caller will wait *until admission*,
// so it's `(position - 1) * avg` — the player at the front of the line
// (position 1) waits roughly until the current admit's turn ends, but
// for the public API we surface 0 as "you're up next, send the WS auth".
// Admitted callers (position == 0) also report 0.
func writeQueueStatus(w http.ResponseWriter, position int, admitted bool) {
	wait := int64(0)
	if position > 1 {
		wait = int64(position-1) * avgTurnMs
	}
	body := struct {
		Position  int   `json:"position"`
		EstWaitMs int64 `json:"est_wait_ms"`
		Admitted  bool  `json:"admitted"`
	}{
		Position:  position,
		EstWaitMs: wait,
		Admitted:  admitted,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// adminQueueJSON returns the current queue + admittedSet for the admin
// panel. queue is sorted oldest-first (= front of line first).
func adminQueueJSON(w http.ResponseWriter, queueEnabled bool, queueDB *sql.DB, adv *advancer.Advancer) {
	w.Header().Set("Content-Type", "application/json")
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
	if !validPlayerID(playerID) {
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
	if !validPlayerID(playerID) {
		http.Error(w, "invalid player_id", http.StatusBadRequest)
		return
	}
	if !adv.ForceFree(playerID) {
		http.Error(w, "not admitted", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminSettingsJSON returns the current lobby_settings the admin
// panel cares about. max_concurrent is always present — the schema
// seeds settings.DefaultMaxConcurrent on first boot.
func adminSettingsJSON(w http.ResponseWriter, lobbyDB *sql.DB) {
	w.Header().Set("Content-Type", "application/json")
	n, err := settings.GetMaxConcurrent(lobbyDB)
	if err != nil {
		log.Printf("admin: settings get: %v", err)
		http.Error(w, "settings read failed", http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"max_concurrent": n,
	})
}

// adminSetMaxConcurrent writes the queue admission cap. Body:
// `{"value": int}`. 0 pauses new admissions without kicking
// already-admitted players; the upper sanity rail of 1024 is far
// above any realistic concurrency we'd run.
func adminSetMaxConcurrent(w http.ResponseWriter, r *http.Request, lobbyDB *sql.DB) {
	var body struct {
		Value int `json:"value"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxQueueBodyBytes)).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if body.Value < 0 || body.Value > 1024 {
		http.Error(w, "value out of range", http.StatusBadRequest)
		return
	}
	if err := settings.SetMaxConcurrent(lobbyDB, body.Value); err != nil {
		log.Printf("admin: settings set: %v", err)
		http.Error(w, "set failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminReplayMetaHandler serves GET /admin/replay/:id/meta — the
// lightweight metadata projection used by the replay viewer's loading
// screen and end screen. Admin-gated by the parent /admin/* wrapper.
//
// Empty `id` (someone hits exactly /admin/replay//meta) is rejected as
// a 400 — the path-suffix matcher in the router doesn't on its own
// reject double slashes.
func adminReplayMetaHandler(w http.ResponseWriter, gameDB *sql.DB, id string) {
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "invalid replay id", http.StatusBadRequest)
		return
	}
	meta, err := leaderboard.GetReplayMeta(gameDB, id)
	if err != nil {
		if errors.Is(err, leaderboard.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("leaderboard: meta %s: %v", id, err)
		http.Error(w, "meta unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meta)
}

// adminReplayTicketHandler serves POST /admin/replay/:id/ticket — mints
// a 30-min HMAC ticket bound to the replay_id, signed with the shared
// admin session secret. Admin-gated by the parent /admin/* wrapper.
//
// We deliberately verify the row exists (and that its checksum is
// intact) before issuing a ticket: a ticket for a missing or corrupted
// row would produce a confusing failure mode at the WS upgrade. Better
// to surface "not found" / "integrity check failed" at the same layer
// the user is reading meta from.
func adminReplayTicketHandler(w http.ResponseWriter, gameDB *sql.DB, id string, secret []byte) {
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "invalid replay id", http.StatusBadRequest)
		return
	}
	if len(secret) < 16 {
		http.Error(w, "ticket secret unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, err := leaderboard.GetReplay(gameDB, id); err != nil {
		if errors.Is(err, leaderboard.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, leaderboard.ErrChecksumMismatch) {
			log.Printf("leaderboard: ticket %s: checksum mismatch", id)
			http.Error(w, "REPLAY_INTEGRITY_FAILED", http.StatusInternalServerError)
			return
		}
		log.Printf("leaderboard: ticket %s: %v", id, err)
		http.Error(w, "replay unavailable", http.StatusInternalServerError)
		return
	}
	exp := time.Now().Add(replayauth.TTL)
	body := struct {
		Ticket    string `json:"ticket"`
		ExpiresAt int64  `json:"expires_at"`
	}{
		Ticket:    replayauth.Sign(secret, id, exp),
		ExpiresAt: exp.UnixMilli(),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
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
			csvSafe(win.DisplayName),
			csvSafe(win.Contact),
			strconv.FormatInt(win.TotalMs, 10),
			win.SubmittedAt,
		})
	}
	cw.Flush()
}

// csvSafe defuses spreadsheet formula injection in user-controlled cell
// values. Excel / Sheets / LibreOffice treat a cell as a formula when:
//   - the first byte is one of `=`, `+`, `-`, `@`, tab, CR, or LF, OR
//   - the first non-whitespace byte is one of `=`, `+`, `-`, `@`
//     (i.e. " =HYPERLINK(...)", "\t+cmd|'...'!A0" — leading whitespace
//     doesn't save us; some apps strip it before parsing).
//
// In either case, prefix the cell with a single quote so the
// spreadsheet renders the literal text. OWASP CSV-injection guidance.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	if isCSVDangerousByte(s[0]) {
		return "'" + s
	}
	if trimmed := strings.TrimLeft(s, " \t\r\n"); trimmed != "" && isFormulaSigil(trimmed[0]) {
		return "'" + s
	}
	return s
}

func isCSVDangerousByte(c byte) bool {
	switch c {
	case '=', '+', '-', '@', '\t', '\r', '\n':
		return true
	}
	return false
}

func isFormulaSigil(c byte) bool {
	switch c {
	case '=', '+', '-', '@':
		return true
	}
	return false
}
