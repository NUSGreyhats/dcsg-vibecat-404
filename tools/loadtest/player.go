package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// runPlayer drives one synthetic player WebSocket from dial through the
// auth+start_run handshake and a steady C2S_INPUT/C2S_SHOOT loop. Returns
// when ctx fires, the server closes the connection, or run completes.
func runPlayer(ctx context.Context, cfg config, m *metrics, playerID string) {
	m.connAttempts.Add(1)
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, resp, err := websocket.Dial(dialCtx, cfg.serverURL+"/ws", nil)
	cancel()
	if err != nil {
		m.connFail.Add(1)
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			m.httpReject.Add(1)
			m.recordReject(fmt.Sprintf("http_%d", resp.StatusCode))
		} else {
			m.dialFail.Add(1)
			m.recordReject("dial:" + classify(err))
		}
		return
	}
	// Allow large state frames (~3 KB typical, may grow with mob counts).
	conn.SetReadLimit(1 << 20)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	// Auth.
	if err := writeC2S(ctx, conn, c2sAuth, 1, authPacket{PlayerID: playerID}); err != nil {
		m.connFail.Add(1)
		m.recordReject("write_auth")
		return
	}
	if err := awaitS2C(ctx, conn, m, s2cAuthOk, 5*time.Second); err != nil {
		m.connFail.Add(1)
		if errors.Is(err, errAuthFailed) {
			m.authFail.Add(1)
		} else {
			m.recordReject("await_auth_ok:" + classify(err))
		}
		return
	}

	// Start the run with the current wall clock; the server validates ±30 min.
	if err := writeC2S(ctx, conn, c2sStartRun, 2, startRunPacket{StartTime: time.Now().UnixMilli()}); err != nil {
		m.connFail.Add(1)
		m.recordReject("write_start_run")
		return
	}
	if err := awaitS2C(ctx, conn, m, s2cRunStarted, 5*time.Second); err != nil {
		m.connFail.Add(1)
		m.recordReject("await_run_started:" + classify(err))
		return
	}

	m.connOK.Add(1)

	// Reader: counts S2C_STATE arrivals and tracks inter-arrival gap.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		readPlayerLoop(ctx, conn, m)
	}()

	// Writer: emits C2S_INPUT at cfg.inputHz, optionally interleaved with
	// C2S_SHOOT. Synthetic motion is a slow circular wander so the engine
	// processes real movement (vs. constant zero input) — closer to load
	// realism for collision/wall checks.
	rngSeed := rand.Uint64()
	rng := rand.New(rand.NewPCG(rngSeed, rngSeed^0x9E3779B97F4A7C15))
	tickInterval := time.Second / time.Duration(maxInt(1, cfg.inputHz))
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	var seq uint32 = 3
	var phase float32 = rng.Float32() * 2 * math.Pi
	for {
		select {
		case <-ctx.Done():
			return
		case <-readerDone:
			return
		case <-ticker.C:
			phase += 0.1
			dx := float32(math.Cos(float64(phase)))
			dy := float32(math.Sin(float64(phase)))
			seq++
			if err := writeC2S(ctx, conn, c2sInput, seq, inputPacket{DX: dx, DY: dy}); err != nil {
				return
			}
			if !cfg.noShoot && rng.IntN(5) == 0 {
				seq++
				_ = writeC2S(ctx, conn, c2sShoot, seq, shootPacket{AimAngle: phase})
			}
		}
	}
}

var errAuthFailed = errors.New("auth_failed")

// readPlayerLoop drains the connection and feeds metrics until close.
// It tracks the gap between consecutive S2C_STATE messages so we can see
// whether the engine is meeting its 50 ms tick budget under load.
func readPlayerLoop(ctx context.Context, conn *websocket.Conn, m *metrics) {
	var lastState time.Time
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			m.disconnects.Add(1)
			return
		}
		m.bytesReceived.Add(int64(len(raw)))
		var env s2cEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			continue
		}
		switch env.Type {
		case s2cState:
			now := time.Now()
			if !lastState.IsZero() {
				m.playerGap.add(float64(now.Sub(lastState).Microseconds()) / 1000.0)
			}
			lastState = now
			m.stateRecv.Add(1)
		case s2cError:
			m.errorRecv.Add(1)
			var ed errorData
			if err := json.Unmarshal(env.Data, &ed); err == nil {
				m.recordReject("s2c_error:" + ed.Code)
			}
		case s2cRunComplete:
			m.runComplete.Add(1)
			m.otherRecv.Add(1)
		default:
			m.otherRecv.Add(1)
		}
	}
}

func writeC2S(ctx context.Context, conn *websocket.Conn, typ uint8, seq uint32, data any) error {
	raw, err := encodeC2S(typ, seq, data)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, raw)
}

// awaitS2C reads until the desired type arrives or the deadline expires.
// It folds AUTH_FAILED into errAuthFailed so the caller can bump a
// dedicated counter. Other S2C_ERROR codes are recorded and we keep
// reading — the server may still send the awaited message right after
// (e.g. a benign "send_auth_first" before our retry).
func awaitS2C(ctx context.Context, conn *websocket.Conn, m *metrics, want uint8, timeout time.Duration) error {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		_, raw, err := conn.Read(deadline)
		if err != nil {
			return err
		}
		m.bytesReceived.Add(int64(len(raw)))
		var env s2cEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			continue
		}
		if env.Type == want {
			return nil
		}
		if env.Type == s2cError {
			var ed errorData
			_ = json.Unmarshal(env.Data, &ed)
			m.errorRecv.Add(1)
			m.recordReject("s2c_error:" + ed.Code)
			if ed.Code == "auth_failed" || ed.Code == "player not registered" || ed.Code == "player not on allowlist" {
				return errAuthFailed
			}
			// Non-fatal handshake error — keep reading.
		}
	}
}

func classify(err error) string {
	if err == nil {
		return "nil"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return "refused"
	case strings.Contains(msg, "deadline exceeded"):
		return "timeout"
	case strings.Contains(msg, "EOF"):
		return "eof"
	case strings.Contains(msg, "reset"):
		return "reset"
	default:
		return "other"
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
