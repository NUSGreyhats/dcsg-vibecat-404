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
	"syscall"
	"time"

	"github.com/coder/websocket"

	"github.com/greyhats/defcon-game/server/internal/auth"
	"github.com/greyhats/defcon-game/server/internal/game"
	"github.com/greyhats/defcon-game/server/internal/identity"
	"github.com/greyhats/defcon-game/server/internal/leaderboard"
	"github.com/greyhats/defcon-game/server/internal/protocol"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	addr := envDefault("LISTEN_ADDR", ":8080")
	dbPath := envDefault("GAME_DB", "game.db")
	identityDBPath := envDefault("IDENTITY_DB", "identity.db")
	allowlistPath := envDefault("ALLOWLIST_PATH", "/data/allowlist.json")
	maxConcurrent := envInt("MAX_CONCURRENT", 64)

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

	identitySvc := identity.NewService(identityDB)

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
		handleWS(ctx, w, r, sem, authCheck, lbSvc)
	})
	mux.HandleFunc("/internal/slot-status", func(w http.ResponseWriter, _ *http.Request) {
		used := len(sem)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"used":` + strconv.Itoa(used) + `,"max":` + strconv.Itoa(maxConcurrent) + `}`))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	// Identity endpoints. Per docs/specs/foundations/03-identity-auth.md the
	// lobby service is spec'd to host these — until that lands, the game
	// server hosts them directly.
	identitySvc.Register(mux)
	mux.HandleFunc("/register", serveRegisterPage)
	// Dev-only: the lobby service is spec'd to host /api/leaderboard, but
	// it doesn't exist yet. Expose a read-through here so local testers
	// can view results without finishing a run.
	mux.HandleFunc("/api/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		limit := 10
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}
		entries := lbSvc.Top(limit)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": entries})
	})

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

func handleWS(ctx context.Context, w http.ResponseWriter, r *http.Request, sem chan struct{}, authCheck func(string) (string, error), lb *leaderboard.Service) {
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

		if engine.PlayerID != "" {
			lb.Unregister(engine.PlayerID)
		}
	}()
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
