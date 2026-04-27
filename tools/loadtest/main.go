// loadtest is a synthetic-client driver for the DEFCON game server. It
// opens N concurrent /ws connections, walks each through the full
// auth+start_run handshake, and emits steady C2S_INPUT (and optional
// C2S_SHOOT) traffic while reading state frames back. Optionally attaches
// extra /ws/spectate viewers per player to stress fan-out bandwidth.
//
// Auth note: the server's AUTH_MODE governs which player_ids are accepted.
// For local stress testing run the server with AUTH_MODE=open. In token
// mode every synthetic ID must be in /data/allowlist.json; in identity
// mode every ID must be pre-registered in identity.db. See README.md.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

type config struct {
	serverURL         string
	players           int
	spectatorsPerGame int
	rampDuration      time.Duration
	testDuration      time.Duration
	inputHz           int
	noShoot           bool
	playerIDPrefix    string
	reportInterval    time.Duration
	spectatorStartLag time.Duration
}

func parseFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.serverURL, "server", "ws://127.0.0.1:8080", "WebSocket server base URL (no trailing path)")
	flag.IntVar(&cfg.players, "players", 10, "number of synthetic players")
	flag.IntVar(&cfg.spectatorsPerGame, "spectators-per-game", 0, "extra /ws/spectate viewers per player (0 disables)")
	flag.DurationVar(&cfg.rampDuration, "ramp", 5*time.Second, "linear ramp-up window for player connections")
	flag.DurationVar(&cfg.testDuration, "duration", 30*time.Second, "total test duration; SIGINT also stops")
	flag.IntVar(&cfg.inputHz, "input-hz", 10, "C2S_INPUT messages per second per player")
	flag.BoolVar(&cfg.noShoot, "no-shoot", false, "disable randomized C2S_SHOOT packets")
	flag.StringVar(&cfg.playerIDPrefix, "id-prefix", "loadtest-", "prefix for synthetic player_ids; suffix is hex-padded to 32 chars")
	flag.DurationVar(&cfg.reportInterval, "report-interval", 5*time.Second, "periodic stats report cadence (0 disables)")
	flag.DurationVar(&cfg.spectatorStartLag, "spectator-lag", 2*time.Second, "delay after a player connects before its spectators attach (lets the run start)")
	flag.Parse()

	cfg.serverURL = strings.TrimRight(cfg.serverURL, "/")
	if cfg.players < 0 {
		cfg.players = 0
	}
	return cfg
}

func main() {
	cfg := parseFlags()
	if cfg.players == 0 && cfg.spectatorsPerGame == 0 {
		fmt.Fprintln(os.Stderr, "error: nothing to do — set -players >0 (and optionally -spectators-per-game)")
		os.Exit(2)
	}

	fmt.Printf("loadtest: %s players=%d spectators/game=%d ramp=%s duration=%s input=%dHz\n",
		cfg.serverURL, cfg.players, cfg.spectatorsPerGame, cfg.rampDuration, cfg.testDuration, cfg.inputHz)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	deadline, cancel := context.WithTimeout(ctx, cfg.testDuration)
	defer cancel()

	m := newMetrics()
	start := time.Now()

	var wg sync.WaitGroup
	playerIDs := make([]string, cfg.players)
	for i := range playerIDs {
		playerIDs[i] = generatePlayerID(cfg.playerIDPrefix, i)
	}

	// Stagger player launches across the ramp window. Spectators are
	// launched after spectatorStartLag from each player launch so the
	// engine has had time to receive C2S_START_RUN and register itself.
	stride := time.Duration(0)
	if cfg.players > 0 {
		stride = cfg.rampDuration / time.Duration(cfg.players)
	}

rampLoop:
	for i, pid := range playerIDs {
		if delay := time.Duration(i) * stride; delay > 0 {
			select {
			case <-deadline.Done():
				break rampLoop
			case <-time.After(delay):
			}
		} else {
			select {
			case <-deadline.Done():
				break rampLoop
			default:
			}
		}
		pid := pid
		wg.Add(1)
		go func() {
			defer wg.Done()
			runPlayer(deadline, cfg, m, pid)
		}()
		for s := 0; s < cfg.spectatorsPerGame; s++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case <-deadline.Done():
					return
				case <-time.After(cfg.spectatorStartLag):
				}
				runSpectator(deadline, cfg, m)
			}()
		}
	}

	if cfg.reportInterval > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(cfg.reportInterval)
			defer ticker.Stop()
			prev := m.snapshot()
			for {
				select {
				case <-deadline.Done():
					return
				case <-ticker.C:
					cur := m.snapshot()
					printDelta(prev, cur)
					prev = cur
				}
			}
		}()
	}

	// Wait for either the natural deadline or signal cancellation, then
	// for all in-flight goroutines to drain. WaitGroup guarantees we don't
	// print final stats while metrics are still being mutated.
	<-deadline.Done()
	wg.Wait()
	final := m.snapshot()
	printFinal(start, final)
}

// generatePlayerID produces a stable-but-unique synthetic ID. The server
// caps player_id length at 64 chars at the WS layer (no explicit limit
// beyond identity.db column width), so prefix+64 hex is safe. We use a
// random suffix so consecutive runs of the load tester don't collide on
// the live-games registry's player_id key.
func generatePlayerID(prefix string, idx int) string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("%s%02d-%s", prefix, idx, hex.EncodeToString(buf[:]))
}
