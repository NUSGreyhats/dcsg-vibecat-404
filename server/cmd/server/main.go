package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"

	"github.com/greyhats/defcon-game/pkg/identity"
	"github.com/greyhats/defcon-game/pkg/netutil"
	"github.com/greyhats/defcon-game/pkg/replayauth"
	"github.com/greyhats/defcon-game/server/internal/auth"
	"github.com/greyhats/defcon-game/server/internal/game"
	"github.com/greyhats/defcon-game/server/internal/leaderboard"
	"github.com/greyhats/defcon-game/server/internal/protocol"
	"github.com/greyhats/defcon-game/server/internal/replay"
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
	replayGlobal := envInt("REPLAY_GLOBAL", 16)
	replayPerIP := envInt("REPLAY_PER_IP", 5)
	// ADMIN_SESSION_SECRET is shared with the lobby. Lobby signs admin
	// session cookies with it; this service uses it to validate replay
	// tickets minted by the lobby (see docs/specs/foundations/08-leaderboard-replay.md).
	// Empty value disables /ws/replay (returns 503 admin disabled), which
	// is correct in dev where AUTH_MODE=identity and admin isn't configured.
	adminSecret := []byte(os.Getenv("ADMIN_SESSION_SECRET"))
	// INTERNAL_SECRET is a shared bearer string between the lobby and
	// the game server's /internal/* endpoints. When set, gateInternal
	// requires X-Internal-Secret to match before checking the private-
	// peer rule. Defense-in-depth so a same-network compromise (a
	// malicious sibling container, a Caddy path-confusion bug) can't
	// reach /internal/* on its own. When empty the secret is not
	// enforced; a startup warning logs it.
	internalSecret := os.Getenv("INTERNAL_SECRET")
	if internalSecret == "" {
		log.Printf("WARN: INTERNAL_SECRET unset — /internal/* relies on private-peer gating only. Set this in prod.")
	}
	// WS_ALLOWED_ORIGINS is a comma-separated list of host patterns
	// (coder/websocket OriginPatterns shape, e.g. "play.example.com,
	// *.example.com,localhost:8000"). When set, /ws and /ws/spectate
	// reject browser upgrades whose Origin doesn't match — closes the
	// cross-site WebSocket vector flagged in issue 71 #2. When empty,
	// origin checking is disabled (the historical posture); a startup
	// warning is logged so prod operators notice.
	wsAllowedOrigins := parseOrigins(os.Getenv("WS_ALLOWED_ORIGINS"))
	if len(wsAllowedOrigins) == 0 {
		log.Printf("WARN: WS_ALLOWED_ORIGINS unset — accepting any Origin on /ws and /ws/spectate. Set this in prod to block cross-site WebSocket upgrades.")
	} else {
		log.Printf("WS origin allowlist: %s", strings.Join(wsAllowedOrigins, ", "))
	}

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

	mode, err := auth.ModeFromEnv()
	if err != nil {
		log.Fatalf("auth mode: %v", err)
	}
	switch mode {
	case auth.ModeOpen:
		log.Printf("auth mode: open")
	case auth.ModeToken:
		log.Printf("auth mode: token (allowlist=%s)", allowlistPath)
	case auth.ModeIdentity:
		log.Printf("auth mode: identity (db=%s)", identityDBPath)
	}
	if len(adminSecret) >= 16 {
		log.Printf("replay viewer: enabled (global=%d per_ip=%d)", replayGlobal, replayPerIP)
	} else {
		log.Printf("replay viewer: disabled (ADMIN_SESSION_SECRET unset; /ws/replay returns 503)")
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

	// Replay viewer caps. Smaller global by default than spectator
	// because /ws/replay is admin-only. Per-IP rate is the same as
	// spectator. Reuses spectator.NewCaps because the admission logic
	// (global counter + per-IP sliding window) is identical.
	replayCaps := spectator.NewCaps(replayGlobal, replayPerIP, time.Minute)

	allowlist := auth.NewAllowlist(allowlistPath)

	authCheck := buildAuthCheck(mode, identityDB, allowlist)

	sem := make(chan struct{}, maxConcurrent)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWS(ctx, w, r, sem, authCheck, lbSvc, liveReg, engineReg, wsAllowedOrigins)
	})
	mux.HandleFunc("/ws/spectate", func(w http.ResponseWriter, r *http.Request) {
		handleSpectateWS(ctx, w, r, engineReg, specCaps, wsAllowedOrigins)
	})
	mux.HandleFunc("/ws/replay", func(w http.ResponseWriter, r *http.Request) {
		handleReplayWS(ctx, w, r, db, adminSecret, replayCaps, wsAllowedOrigins)
	})
	mux.HandleFunc("/internal/slot-status", func(w http.ResponseWriter, r *http.Request) {
		if !gateInternal(w, r, internalSecret) {
			return
		}
		used := len(sem)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"used":` + strconv.Itoa(used) + `,"max":` + strconv.Itoa(maxConcurrent) + `}`))
	})
	mux.HandleFunc("/internal/live-games", func(w http.ResponseWriter, r *http.Request) {
		if !gateInternal(w, r, internalSecret) {
			return
		}
		// Polled every 2 s by the lobby and cached in-process. Returns
		// [] (not null) when no runs are live so the lobby's selector
		// renders an explicit empty state.
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
		Addr:    addr,
		Handler: mux,
		// HTTP-side timeouts. WebSocket connections are hijacked by
		// coder/websocket on Accept, so these only apply to /health,
		// /internal/*, and the upgrade handshake itself — they don't
		// kill in-flight WS sessions. Without them a slow client could
		// hold a handler goroutine open indefinitely (slowloris).
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
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

// buildAuthCheck returns the WS-time auth gate. Token mode still gates on
// the lobby-written allowlist, but also resolves the canonical display name
// from identity.db so public surfaces never show raw player_id values.
func buildAuthCheck(mode auth.Mode, identityDB *sql.DB, allowlist *auth.Allowlist) func(string) (string, error) {
	return func(playerID string) (string, error) {
		switch mode {
		case auth.ModeIdentity:
			return lookupDisplayName(identityDB, playerID)
		case auth.ModeToken:
			if err := auth.Check(mode, allowlist, playerID); err != nil {
				return "", err
			}
			return lookupDisplayName(identityDB, playerID)
		default:
			return "", auth.Check(mode, allowlist, playerID)
		}
	}
}

// errAuthUnavailable is returned to the client when an identity.db lookup
// fails for any reason other than ErrNotFound. The underlying driver/SQL
// error is logged server-side; the client only sees a generic string so
// internal storage details (locks, query context, file paths) don't leak
// over the WS auth_failed envelope.
var errAuthUnavailable = errors.New("auth unavailable")

func lookupDisplayName(identityDB *sql.DB, playerID string) (string, error) {
	p, err := identity.Lookup(identityDB, playerID)
	if err != nil {
		if errors.Is(err, identity.ErrNotFound) {
			return "", errors.New("player not registered")
		}
		log.Printf("auth: identity lookup for %s: %v", playerID, err)
		return "", errAuthUnavailable
	}
	return p.DisplayName, nil
}

func handleWS(ctx context.Context, w http.ResponseWriter, r *http.Request, sem chan struct{}, authCheck func(string) (string, error), lb *leaderboard.Service, liveReg *spectator.LiveRegistry, engineReg *spectator.Engines, originPatterns []string) {
	select {
	case sem <- struct{}{}:
	default:
		http.Error(w, "server full", http.StatusServiceUnavailable)
		return
	}
	releaseSlot := func() { <-sem }

	conn, err := acceptWS(w, r, originPatterns)
	if err != nil {
		releaseSlot()
		log.Printf("ws accept: %v", err)
		return
	}
	// Cap any single inbound frame. All legitimate C2S packets fit
	// comfortably; an oversized frame is treated as protocol abuse and
	// closes the connection at conn.Read time.
	conn.SetReadLimit(game.MaxClientFrameBytes)

	connCtx, cancel := context.WithCancel(ctx)

	inCh := make(chan []byte, game.InChBuf)
	outCh := make(chan protocol.S2CEnvelope, game.OutChBuf)

	engine := game.NewEngine(inCh, outCh, lb.Submit, authCheck)
	engine.Live = liveReg
	engine.Engines = engineReg

	writerDone := make(chan struct{})
	go connReader(connCtx, conn, inCh, cancel)
	go func() {
		defer close(writerDone)
		connWriter(connCtx, conn, outCh, cancel)
	}()

	go func() {
		defer cancel()
		defer releaseSlot()

		engine.Run(connCtx)

		// Engine has emitted any final messages (notably RUN_COMPLETE on
		// death). Close outCh so the writer drains the buffer instead of
		// being killed mid-flush by conn.Close, then wait briefly for it
		// to finish so the client actually sees the terminal envelope.
		close(outCh)
		select {
		case <-writerDone:
		case <-time.After(500 * time.Millisecond):
		}
		conn.Close(websocket.StatusNormalClosure, "bye")

		// Leaderboard registry is keyed by player_id (the WS-auth
		// identity), but live-games and spectator routing are keyed
		// by the public run_id minted in initRun. Use each accordingly.
		if engine.PlayerID != "" {
			lb.Unregister(engine.PlayerID)
		}
		if rid := engine.RunID(); rid != "" {
			liveReg.Remove(rid)
		}
	}()
}

// SpectatorPingInterval is the cadence the server-initiated keepalive
// ping fires at on a spectator connection. The coder/websocket library
// does not auto-ping; if we relied on inbound traffic to detect liveness
// we'd close every spectator session after a few minutes of correctly
// silent (read-only) behaviour.
const SpectatorPingInterval = 30 * time.Second

// SpectatorPingTimeout is the per-ping deadline. If the pong doesn't
// come back inside this window we treat the connection as dead and
// close it. Spec says ~60 s idle ceiling; 30 s + 10 s ≈ that with
// retry headroom.
const SpectatorPingTimeout = 10 * time.Second

// SpectatorRegisterTimeout caps how long the accept handler waits on the
// engine's register reply before giving up and returning 503. Shorter
// than the engine's tick (50 ms) would race the drain; 1 s gives many
// ticks of slack while still preventing zombie pending accepts.
const SpectatorRegisterTimeout = 1 * time.Second

// WSWriteTimeout caps each WebSocket write. A peer that can't accept a
// frame inside this window is treated as dead — closing the connection
// frees the writer goroutine and any backing buffers. Game and replay
// writers can otherwise be pinned indefinitely by a slow-reading peer
// (the websocket library queues frames until the underlying TCP buffer
// drains).
const WSWriteTimeout = 5 * time.Second

// InternalSecretHeader is the request header carrying the shared
// /internal/* bearer secret between lobby and game server. Caller and
// callee resolve the value from INTERNAL_SECRET env.
const InternalSecretHeader = "X-Internal-Secret"

// gateInternal enforces that /internal/* endpoints are only callable
// by trusted private peers (loopback or docker-network private IPs)
// AND, when secret is non-empty, by callers that present a matching
// X-Internal-Secret header. Returns true if the request should proceed;
// otherwise writes a 403 or 405 response and returns false.
func gateInternal(w http.ResponseWriter, r *http.Request, secret string) bool {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !netutil.IsPrivatePeer(host) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	if secret != "" {
		// Constant-time compare so a side-channel can't reveal the
		// secret one byte at a time.
		got := r.Header.Get(InternalSecretHeader)
		if subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return false
		}
	}
	return true
}

func handleSpectateWS(ctx context.Context, w http.ResponseWriter, r *http.Request, engineReg *spectator.Engines, caps *spectator.Caps, originPatterns []string) {
	runID := r.URL.Query().Get("run_id")
	if runID == "" {
		http.Error(w, "run_id required", http.StatusBadRequest)
		return
	}

	if !caps.AllowIP(netutil.ClientIP(r), time.Now()) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}

	if !caps.AcquireGlobal() {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "spectator capacity reached", http.StatusServiceUnavailable)
		return
	}
	// The global slot is released by the writer goroutine on exit. Until
	// then, every early-return path explicitly releases inline. Carrying
	// the slot via a deferred release on the handler frame would be
	// wrong: the handler returns immediately after spawning goroutines,
	// so the slot would be released for the entire spectator session
	// (defeating the cap).

	registerCh, ok := engineReg.Lookup(runID)
	if !ok {
		caps.ReleaseGlobal()
		http.Error(w, "no live run for run_id", http.StatusNotFound)
		return
	}

	// bestEffortDeregister tells the engine to drop this Conn from its
	// spectator slice on the next tick. Non-blocking so a saturated
	// RegisterCh can't stall the handler; the engine cleans up on run
	// end anyway.
	conn := spectator.NewConn()
	bestEffortDeregister := func() {
		select {
		case registerCh <- spectator.Req{Action: spectator.ActionDeregister, Conn: conn}:
		default:
		}
	}

	reply := make(chan bool, 1)
	select {
	case registerCh <- spectator.Req{Action: spectator.ActionRegister, Conn: conn, Reply: reply}:
	case <-time.After(SpectatorRegisterTimeout):
		caps.ReleaseGlobal()
		http.Error(w, "engine busy", http.StatusServiceUnavailable)
		return
	case <-ctx.Done():
		caps.ReleaseGlobal()
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	// From this point the engine *might* drain the register and accept us;
	// every early return must send a deregister so we don't leak a
	// per-game slot on the engine.
	var accepted bool
	select {
	case accepted = <-reply:
	case <-time.After(SpectatorRegisterTimeout):
		bestEffortDeregister()
		caps.ReleaseGlobal()
		http.Error(w, "engine busy", http.StatusServiceUnavailable)
		return
	case <-ctx.Done():
		bestEffortDeregister()
		caps.ReleaseGlobal()
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	if !accepted {
		caps.ReleaseGlobal()
		w.Header().Set("Retry-After", "5")
		http.Error(w, "spectator capacity reached", http.StatusServiceUnavailable)
		return
	}

	wsConn, err := acceptWS(w, r, originPatterns)
	if err != nil {
		log.Printf("spectate accept: %v", err)
		bestEffortDeregister()
		caps.ReleaseGlobal()
		return
	}

	// Ownership transfer point. From here on, the writer goroutine owns
	// the global cap slot release and the engine deregister; the reader
	// goroutine and the pinger goroutine both signal failure via cancel.
	connCtx, cancel := context.WithCancel(ctx)

	// Reader: spectator connections are read-only. Any frame at all closes
	// the WS with code 1008 (policy violation) per
	// docs/specs/foundations/02-protocol.md#read-only-enforcement.
	// No per-read deadline — silence is the *normal* state. Liveness is
	// detected by the pinger goroutine instead.
	go func() {
		defer cancel()
		wsConn.SetReadLimit(1024)
		for {
			_, _, readErr := wsConn.Read(connCtx)
			if readErr != nil {
				return
			}
			env := protocol.NewS2C(protocol.S2CError, 0, protocol.ErrorData{
				Code:    "SPECTATOR_READ_ONLY",
				Message: "spectator connections are read-only",
			})
			if raw, mErr := json.Marshal(env); mErr == nil {
				_ = wsConn.Write(connCtx, websocket.MessageText, raw)
			}
			_ = wsConn.Close(websocket.StatusPolicyViolation, "spectator_read_only")
			return
		}
	}()

	// Pinger: server-initiated keepalive. The coder/websocket library
	// does not auto-ping; without this, a correctly-silent spectator
	// session would never get checked for liveness and a half-open WS
	// could outlive the actual TCP connection.
	go func() {
		defer cancel()
		t := time.NewTicker(SpectatorPingInterval)
		defer t.Stop()
		for {
			select {
			case <-connCtx.Done():
				return
			case <-t.C:
				pingCtx, pingCancel := context.WithTimeout(connCtx, SpectatorPingTimeout)
				pingErr := wsConn.Ping(pingCtx)
				pingCancel()
				if pingErr != nil {
					return
				}
			}
		}
	}()

	// Writer: drains conn.OutCh until the engine closes it. Owns the
	// final cleanup — releases the global cap slot and best-effort
	// deregisters with the engine on exit.
	go func() {
		defer cancel()
		defer caps.ReleaseGlobal()
		defer wsConn.Close(websocket.StatusNormalClosure, "bye")
		defer bestEffortDeregister()
		// Mark the Conn closed so the engine's per-tick prune drops it
		// even if the deregister send above hits a full channel. Issue
		// 71 #15: without this, a saturated RegisterCh leaks per-game
		// spectator slots until the run ends.
		defer conn.MarkClosed()
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
				writeCtx, writeCancel := context.WithTimeout(connCtx, WSWriteTimeout)
				err = wsConn.Write(writeCtx, websocket.MessageText, raw)
				writeCancel()
				if err != nil {
					return
				}
			}
		}
	}()
}

// uuidPattern is the canonical UUID-v4 hex shape with dashes. Loosely
// matched (no version-bit assertions) — `leaderboard.GetReplay` is the
// real authority on whether a row exists; this is just first-pass
// validation to keep malformed IDs out of the SQL parameter binder.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// handleReplayWS gates the replay viewer WS. Flow:
//  1. Validate replay_id shape and ticket HMAC (admin auth surrogate).
//  2. Acquire global cap + per-IP rate slot (release on failure paths).
//  3. Load the run from game.db (checksum-verified by leaderboard.GetReplay).
//  4. Decode the stored input_log blob into []game.InputRecord.
//  5. Spawn the replay.Player and bridge it onto the WS:
//     - reader: accept C2S_REPLAY_CONTROL only; any other type → close with REPLAY_READ_ONLY.
//     - writer: drain Player.OutCh → JSON → WS.
//     - pinger: 30s keepalive (mirrors spectator).
//
// All four goroutines share connCtx; cancel from any one tears the rest down.
func handleReplayWS(parent context.Context, w http.ResponseWriter, r *http.Request, db *sql.DB, adminSecret []byte, caps *spectator.Caps, originPatterns []string) {
	if len(adminSecret) < 16 {
		http.Error(w, "admin disabled", http.StatusServiceUnavailable)
		return
	}
	replayID := r.URL.Query().Get("replay_id")
	ticket := r.URL.Query().Get("ticket")
	if !uuidPattern.MatchString(replayID) {
		http.Error(w, "invalid replay_id", http.StatusBadRequest)
		return
	}
	if err := replayauth.Verify(adminSecret, ticket, replayID, time.Now()); err != nil {
		w.Header().Set("WWW-Authenticate", "Replay")
		if errors.Is(err, replayauth.ErrExpired) {
			http.Error(w, "ticket expired", http.StatusUnauthorized)
		} else {
			http.Error(w, "invalid ticket", http.StatusUnauthorized)
		}
		return
	}

	if !caps.AllowIP(netutil.ClientIP(r), time.Now()) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	if !caps.AcquireGlobal() {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "replay capacity reached", http.StatusServiceUnavailable)
		return
	}

	rep, err := leaderboard.GetReplay(db, replayID)
	if err != nil {
		caps.ReleaseGlobal()
		if errors.Is(err, leaderboard.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, leaderboard.ErrChecksumMismatch) {
			log.Printf("replay: %s: checksum mismatch", replayID)
			http.Error(w, "REPLAY_INTEGRITY_FAILED", http.StatusInternalServerError)
			return
		}
		log.Printf("replay: %s: load: %v", replayID, err)
		http.Error(w, "replay unavailable", http.StatusInternalServerError)
		return
	}

	var records []game.InputRecord
	if err := json.Unmarshal(rep.InputLog, &records); err != nil {
		caps.ReleaseGlobal()
		log.Printf("replay: %s: unmarshal log: %v", replayID, err)
		http.Error(w, "replay corrupt", http.StatusInternalServerError)
		return
	}

	wsConn, err := acceptWS(w, r, originPatterns)
	if err != nil {
		caps.ReleaseGlobal()
		log.Printf("replay: accept: %v", err)
		return
	}

	connCtx, cancel := context.WithCancel(parent)
	player := replay.New(rep.PlayerID, rep.DisplayName, rep.Seed, records)

	// Reader goroutine: accept C2S_REPLAY_CONTROL only. Any other C2S
	// type closes the WS with REPLAY_READ_ONLY (mirrors spectator's
	// SPECTATOR_READ_ONLY enforcement).
	go func() {
		defer cancel()
		// Cap inbound frame size — control packets are tiny ({"action": "set_speed", "speed": 8.0} ≈ 35 bytes).
		wsConn.SetReadLimit(1024)
		for {
			_, raw, readErr := wsConn.Read(connCtx)
			if readErr != nil {
				return
			}
			env, decErr := protocol.DecodeEnvelope(raw)
			if decErr != nil {
				continue
			}
			if env.Type != protocol.C2SReplayControl {
				writeReplayError(connCtx, wsConn, "REPLAY_READ_ONLY", "replay connections accept C2S_REPLAY_CONTROL only")
				_ = wsConn.Close(websocket.StatusPolicyViolation, "replay_read_only")
				return
			}
			var pkt protocol.ReplayControlPacket
			if err := json.Unmarshal(env.Data, &pkt); err != nil {
				continue
			}
			switch pkt.Action {
			case "play":
				player.Play()
			case "pause":
				player.Pause()
			case "set_speed":
				player.SetSpeed(pkt.Speed)
			case "restart":
				player.Restart()
			default:
				// Unknown action: ignore. Tolerant so a future client
				// with a new verb can hit an old server without dying.
			}
		}
	}()

	// Pinger: same shape as spectator's keepalive.
	go func() {
		defer cancel()
		t := time.NewTicker(SpectatorPingInterval)
		defer t.Stop()
		for {
			select {
			case <-connCtx.Done():
				return
			case <-t.C:
				pingCtx, pingCancel := context.WithTimeout(connCtx, SpectatorPingTimeout)
				pingErr := wsConn.Ping(pingCtx)
				pingCancel()
				if pingErr != nil {
					return
				}
			}
		}
	}()

	// Player goroutine: drives the engine; closes player.OutCh on exit,
	// which the writer below uses as its termination signal.
	go player.Run(connCtx)

	// Writer: drain player.OutCh → WS. Owns the cleanup (cap release,
	// WS close, ctx cancel). When player.OutCh closes, we wait briefly
	// for the WS to flush the final RUN_COMPLETE before tearing down.
	go func() {
		defer cancel()
		defer caps.ReleaseGlobal()
		defer wsConn.Close(websocket.StatusNormalClosure, "bye")
		for {
			select {
			case <-connCtx.Done():
				return
			case env, ok := <-player.OutCh:
				if !ok {
					// Player exited; give the WS a 500ms grace window
					// (mirrors SpectatorCatchupGrace) for any final
					// frames already in flight to land.
					select {
					case <-time.After(game.SpectatorCatchupGrace):
					case <-connCtx.Done():
					}
					return
				}
				raw, err := json.Marshal(env)
				if err != nil {
					continue
				}
				writeCtx, writeCancel := context.WithTimeout(connCtx, WSWriteTimeout)
				err = wsConn.Write(writeCtx, websocket.MessageText, raw)
				writeCancel()
				if err != nil {
					return
				}
			}
		}
	}()
}

// writeReplayError pushes a single S2C_ERROR envelope before the caller
// closes the WS. Best-effort — if the write fails, the close that
// follows immediately makes the loss invisible to the viewer (which
// would also see the close anyway).
func writeReplayError(ctx context.Context, ws *websocket.Conn, code, msg string) {
	env := protocol.NewS2C(protocol.S2CError, 0, protocol.ErrorData{
		Code:    code,
		Message: msg,
	})
	raw, err := json.Marshal(env)
	if err != nil {
		return
	}
	_ = ws.Write(ctx, websocket.MessageText, raw)
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
			writeCtx, writeCancel := context.WithTimeout(ctx, WSWriteTimeout)
			err = conn.Write(writeCtx, websocket.MessageText, raw)
			writeCancel()
			if err != nil {
				return
			}
		}
	}
}

// parseOrigins splits a comma-separated WS_ALLOWED_ORIGINS env value
// into the OriginPatterns slice that coder/websocket expects. Trims
// whitespace; drops empty entries so a trailing comma doesn't widen
// the allowlist to "" (which would silently match nothing).
func parseOrigins(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// acceptWS centralises the websocket.Accept call so all three WS
// endpoints share Origin handling. When origins is nil/empty,
// InsecureSkipVerify=true preserves the historical accept-anything
// posture (a startup warning logs it). When set, OriginPatterns
// constrains which Origin headers browser upgrades may carry; non-
// browser callers (no Origin header) are still accepted.
func acceptWS(w http.ResponseWriter, r *http.Request, origins []string) (*websocket.Conn, error) {
	opts := &websocket.AcceptOptions{}
	if len(origins) == 0 {
		opts.InsecureSkipVerify = true
	} else {
		opts.OriginPatterns = origins
	}
	return websocket.Accept(w, r, opts)
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
