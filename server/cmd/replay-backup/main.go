// replay-backup is a localhost-only operator tool that lets you watch
// a single recorded run via the fixed replay engine without touching
// the production game server.
//
// Workflow:
//
//  1. Operator downloads a run from the lobby admin endpoint:
//     curl -b admin_session=… https://<lobby>/admin/replay/<id> > run.json
//  2. Operator runs `replay-backup -client-dist=path/to/client/dist`.
//  3. Operator opens http://localhost:8080, uploads run.json, follows
//     the redirect to /admin/replay/<id>/view, watches the replay.
//
// Reuses the production replay.Player (server/internal/replay) so the
// PR #96 inCh-sizing fix is automatically inherited. Skips the admin
// auth gate — the binary listens on localhost only by default.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/greyhats/defcon-game/server/internal/game"
)

// storedRun holds a single uploaded run plus its decoded input log.
// We decode the InputLog at upload time to fail loudly on a malformed
// file rather than at WS-upgrade time when the operator is already
// waiting for the viewer to load.
type storedRun struct {
	ReplayID    string
	PlayerID    string
	DisplayName string
	TotalMs     int64
	Seed        int64
	Records     []game.InputRecord
	UploadedAt  time.Time
}

// store is a tiny mutex-guarded map keyed by replay_id. The backup
// binary is single-process / single-operator, so a map suffices —
// no LRU or persistence.
type store struct {
	mu   sync.RWMutex
	runs map[string]*storedRun
}

func newStore() *store {
	return &store{runs: make(map[string]*storedRun)}
}

func (s *store) put(r *storedRun) {
	s.mu.Lock()
	s.runs[r.ReplayID] = r
	s.mu.Unlock()
}

func (s *store) get(id string) (*storedRun, bool) {
	s.mu.RLock()
	r, ok := s.runs[id]
	s.mu.RUnlock()
	return r, ok
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address (default: localhost only — bind 0.0.0.0 explicitly to expose)")
	clientDist := flag.String("client-dist", "./dist", "path to the Vite-built client/dist directory containing replay.html and assets/")
	flag.Parse()

	// Local HMAC secret. Generated fresh per process; never persisted.
	// Used by both the ticket-mint endpoint and the WS upgrade verifier,
	// so locally-minted tickets validate locally.
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		log.Fatalf("generate secret: %v", err)
	}

	st := newStore()

	mux := http.NewServeMux()
	registerRoutes(mux, st, secret, *clientDist)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		log.Printf("replay-backup listening on http://%s (client-dist=%s)", *addr, *clientDist)
		log.Printf("upload run JSON at http://%s/", *addr)
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
