package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"

	"github.com/greyhats/defcon-game/pkg/identity"
	"github.com/greyhats/defcon-game/server/internal/auth"
	"github.com/greyhats/defcon-game/server/internal/game"
	"github.com/greyhats/defcon-game/server/internal/leaderboard"
	"github.com/greyhats/defcon-game/server/internal/protocol"
	"github.com/greyhats/defcon-game/server/internal/spectator"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	addr := envDefault("LISTEN_ADDR", ":8080")
	dbPath := envDefault("GAME_DB", "game.db")
	identityDBPath := envDefault("IDENTITY_DB", "identity.db")
	allowlistPath := envDefault("ALLOWLIST_PATH", "/data/allowlist.json")
	maxConcurrent := envInt("MAX_CONCURRENT", 64)
	spectatorGlobal := envInt("SPECTATOR_GLOBAL", 200)
	spectatorPerIP := envInt("SPECTATOR_PER_IP", 5)

	db, err := leaderboard.OpenDB(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	identityDB, err := identity.OpenDB(identityDBPath)
	if err != nil {
		log.Fatalf("open identity db: %v", err)
	}
	defer identityDB.Close()

	mode := auth.ModeFromEnv()
	switch mode {
	case auth.ModeOpen:
		log.Printf("auth mode: open")
	case auth.ModeToken:
		log.Printf("auth mode: token (allowlist=%s)", allowlistPath)
	case auth.ModeIdentity:
		log.Printf("auth mode: identity (db=%s)", identityDBPath)
	}

	lbSvc := leaderboard.NewService(db)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	lbSvc.Start(ctx)

	// Live-games registry — engines push a Summary every tick; the
	// /internal/live-games endpoint serves the aggregated snapshot to
	// the lobby's spectator selector. See
	// docs/specs/foundations/05-server-runtime.md#internal-endpoint.
	liveReg := spectator.NewLiveRegistry(maxConcurrent)
	liveReg.Start(ctx)

	// Spectator engines registry + global+per-IP caps. The accept handler
	// for /ws/spectate looks engines up by player_id from the URL and
	// dispatches a register request. Per-game cap is enforced by each
	// engine on the receiving side.
	engineReg := spectator.NewEngines()
	specCaps := spectator.NewCaps(spectatorGlobal, spectatorPerIP, time.Minute)

	allowlist := auth.NewAllowlist(allowlistPath)

	// Build the auth gate once so the WS handler doesn't re-derive it per
	// connection. Identity mode requires the player_id to be present in
	// identity.db — i.e. the player must have registered — and returns
	// the stored display_name so the leaderboard sees the human name
	// rather than the raw hex ID.
	authCheck := func(playerID string) (string, error) {
		switch mode {
		case auth.ModeIdentity:
			p, err := identity.Lookup(identityDB, playerID)
			if err != nil {
				if errors.Is(err, identity.ErrNotFound) {
					return "", errors.New("player not registered")
				}
				return "", err
			}
			return p.DisplayName, nil
		default:
			return "", auth.Check(mode, allowlist, playerID)
		}
	}

	sem := make(chan struct{}, maxConcurrent)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWS(ctx, w, r, sem, authCheck, lbSvc, liveReg, engineReg)
	})
	mux.HandleFunc("/ws/spectate", func(w http.ResponseWriter, r *http.Request) {
		handleSpectateWS(ctx, w, r, engineReg, specCaps)
	})
	mux.HandleFunc("/internal/slot-status", func(w http.ResponseWriter, _ *http.Request) {
		used := len(sem)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"used":` + strconv.Itoa(used) + `,"max":` + strconv.Itoa(maxConcurrent) + `}`))
	})
	mux.HandleFunc("/internal/live-games", func(w http.ResponseWriter, _ *http.Request) {
		// Loopback-only consumption — the lobby polls this every 2 s and
		// caches in-process. Returns [] (not null) when no runs are live so
		// the lobby's selector renders an explicit empty state.
		entries := liveReg.Snapshot()
		if entries == nil {
			entries = []spectator.Summary{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entries)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	// Identity HTTP endpoints, /register, /api/leaderboard, and /api/replay/:id
	// all live on the lobby service. The game server still imports
	// pkg/identity for WS-time Lookup in authCheck above and writes to
	// game.db via the leaderboard goroutine started below.

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}

func handleWS(ctx context.Context, w http.ResponseWriter, r *http.Request, sem chan struct{}, authCheck func(string) (string, error), lb *leaderboard.Service, liveReg *spectator.LiveRegistry, engineReg *spectator.Engines) {
	select {
	case sem <- struct{}{}:
	default:
		http.Error(w, "server full", http.StatusServiceUnavailable)
		return
	}
	releaseSlot := func() { <-sem }

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		releaseSlot()
		log.Printf("ws accept: %v", err)
		return
	}

	connCtx, cancel := context.WithCancel(ctx)

	inCh := make(chan []byte, game.InChBuf)
	outCh := make(chan protocol.S2CEnvelope, game.OutChBuf)

	engine := game.NewEngine(inCh, outCh, lb.Submit, authCheck)
	engine.Live = liveReg
	engine.Engines = engineReg

	go connReader(connCtx, conn, inCh, cancel)
	go connWriter(connCtx, conn, outCh, cancel)

	go func() {
		defer cancel()
		defer releaseSlot()
		defer conn.Close(websocket.StatusNormalClosure, "bye")

		engine.Run(connCtx)

		if engine.PlayerID != "" {
			lb.Unregister(engine.PlayerID)
			liveReg.Remove(engine.PlayerID)
		}
	}()
}

// SpectatorIdleTimeout closes a spectator WS that hasn't sent a control
// frame (pong) in this long. We rely on the WS library's idle ping/pong
// — there are no app-level pings from spectators (they're read-only).
const SpectatorIdleTimeout = 60 * time.Second

// SpectatorRegisterTimeout caps how long the accept handler waits on the
// engine's register reply before giving up and returning 503. Shorter
// than the engine's tick (50 ms) would race the drain; 1 s gives many
// ticks of slack while still preventing zombie pending accepts.
const SpectatorRegisterTimeout = 1 * time.Second

func handleSpectateWS(ctx context.Context, w http.ResponseWriter, r *http.Request, engineReg *spectator.Engines, caps *spectator.Caps) {
	playerID := r.URL.Query().Get("player_id")
	if playerID == "" {
		http.Error(w, "player_id required", http.StatusBadRequest)
		return
	}

	if !caps.AllowIP(clientIP(r), time.Now()) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}

	if !caps.AcquireGlobal() {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "spectator capacity reached", http.StatusServiceUnavailable)
		return
	}
	released := false
	releaseGlobal := func() {
		if !released {
			released = true
			caps.ReleaseGlobal()
		}
	}
	defer releaseGlobal()

	registerCh, ok := engineReg.Lookup(playerID)
	if !ok {
		http.Error(w, "no live run for player_id", http.StatusNotFound)
		return
	}

	conn := spectator.NewConn()
	reply := make(chan bool, 1)
	select {
	case registerCh <- spectator.Req{Action: spectator.ActionRegister, Conn: conn, Reply: reply}:
	case <-time.After(SpectatorRegisterTimeout):
		http.Error(w, "engine busy", http.StatusServiceUnavailable)
		return
	case <-ctx.Done():
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	var accepted bool
	select {
	case accepted = <-reply:
	case <-time.After(SpectatorRegisterTimeout):
		http.Error(w, "engine busy", http.StatusServiceUnavailable)
		return
	case <-ctx.Done():
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	if !accepted {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "spectator capacity reached", http.StatusServiceUnavailable)
		return
	}
	// From here on the engine owns conn; the global slot is released only
	// when the connWriter exits.

	wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("spectate accept: %v", err)
		// Best-effort deregister + slot release; engine already accepted.
		select {
		case registerCh <- spectator.Req{Action: spectator.ActionDeregister, Conn: conn}:
		default:
		}
		return
	}

	connCtx, cancel := context.WithCancel(ctx)

	// Reader: spectator connections are read-only. Any frame at all closes
	// the WS with code 1008 (policy violation) per
	// docs/specs/foundations/02-protocol.md#read-only-enforcement.
	go func() {
		defer cancel()
		for {
			wsConn.SetReadLimit(1024)
			readCtx, readCancel := context.WithTimeout(connCtx, SpectatorIdleTimeout)
			_, _, err := wsConn.Read(readCtx)
			readCancel()
			if err != nil {
				return
			}
			// Any inbound frame is a programming error or an attack. Send
			// the typed error then close 1008.
			env := protocol.NewS2C(protocol.S2CError, 0, protocol.ErrorData{
				Code:    "SPECTATOR_READ_ONLY",
				Message: "spectator connections are read-only",
			})
			if raw, err := json.Marshal(env); err == nil {
				_ = wsConn.Write(connCtx, websocket.MessageText, raw)
			}
			_ = wsConn.Close(websocket.StatusPolicyViolation, "spectator_read_only")
			return
		}
	}()

	// Writer: drains conn.OutCh until close. On exit, deregister from the
	// engine (if not already gone) and release the global slot.
	go func() {
		defer cancel()
		defer releaseGlobal()
		defer wsConn.Close(websocket.StatusNormalClosure, "bye")
		defer func() {
			select {
			case registerCh <- spectator.Req{Action: spectator.ActionDeregister, Conn: conn}:
			default:
			}
		}()
		for {
			select {
			case <-connCtx.Done():
				return
			case env, ok := <-conn.OutCh:
				if !ok {
					return // engine closed our channel — clean shutdown
				}
				raw, err := json.Marshal(env)
				if err != nil {
					continue
				}
				if err := wsConn.Write(connCtx, websocket.MessageText, raw); err != nil {
					return
				}
			}
		}
	}()
}

// clientIP returns the best-effort source IP for rate-limit bookkeeping.
// Honours X-Forwarded-For (set by the Caddy reverse proxy) and falls
// back to RemoteAddr.
func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		if i := strings.IndexByte(xf, ','); i > 0 {
			return strings.TrimSpace(xf[:i])
		}
		return strings.TrimSpace(xf)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func connReader(ctx context.Context, conn *websocket.Conn, inCh chan<- []byte, cancel context.CancelFunc) {
	defer cancel()
	defer close(inCh)
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		select {
		case inCh <- data:
		case <-ctx.Done():
			return
		}
	}
}

func connWriter(ctx context.Context, conn *websocket.Conn, outCh <-chan protocol.S2CEnvelope, cancel context.CancelFunc) {
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-outCh:
			if !ok {
				return
			}
			raw, err := json.Marshal(env)
			if err != nil {
				log.Printf("marshal: %v", err)
				continue
			}
			if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
				return
			}
		}
	}
}

func envDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
