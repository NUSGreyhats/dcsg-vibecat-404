package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// runSpectator opens a /ws/spectate connection and drains state/runtime
// frames until the run ends or ctx fires. Spectators are read-only — any
// frame back to the server triggers a 1008 close, so we simply pump
// reads.
//
// As of issue #71 #1 the spectator URL takes a public per-run run_id
// instead of player_id. The loadtest doesn't see run_ids directly (they
// are minted server-side and surfaced only via /api/live), so we poll
// the lobby's public live-games feed to discover one. We don't need a
// 1:1 mapping with synthetic players — under load, picking any live
// run_id is sufficient to exercise spectator fan-out.
func runSpectator(ctx context.Context, cfg config, m *metrics) {
	m.connAttempts.Add(1)

	runID, err := pickLiveRunID(ctx, cfg)
	if err != nil {
		m.connFail.Add(1)
		m.recordReject("spec_no_live:" + classify(err))
		return
	}

	u := cfg.serverURL + "/ws/spectate?run_id=" + url.QueryEscape(runID)

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, resp, err := websocket.Dial(dialCtx, u, nil)
	cancel()
	if err != nil {
		m.connFail.Add(1)
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			m.httpReject.Add(1)
			m.recordReject(fmt.Sprintf("spec_http_%d", resp.StatusCode))
		} else {
			m.dialFail.Add(1)
			m.recordReject("spec_dial:" + classify(err))
		}
		return
	}
	conn.SetReadLimit(1 << 20)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	m.connOK.Add(1)

	var lastState time.Time
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			if !isExpectedClose(ctx, err) {
				m.disconnects.Add(1)
			}
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
				m.spectatorGap.add(float64(now.Sub(lastState).Microseconds()) / 1000.0)
			}
			lastState = now
			m.stateRecv.Add(1)
		case s2cError:
			m.errorRecv.Add(1)
			var ed errorData
			if err := json.Unmarshal(env.Data, &ed); err == nil {
				m.recordReject("spec_s2c_error:" + ed.Code)
			}
		case s2cRunComplete:
			m.runComplete.Add(1)
		default:
			m.otherRecv.Add(1)
		}
	}
}

// pickLiveRunID polls the lobby's /api/live until at least one run is
// listed, then returns one at random. Bounded by ctx; bails out with
// the last error if the deadline is hit before any run shows up. The
// lobby URL is derived from cfg.serverURL by swapping the game-server
// scheme/host with the public origin (the loadtest is pointed at the
// game server but /api/live lives on the lobby behind the same proxy).
func pickLiveRunID(ctx context.Context, cfg config) (string, error) {
	api := liveAPIURL(cfg.serverURL)
	deadline := time.Now().Add(15 * time.Second)
	client := &http.Client{Timeout: 2 * time.Second}
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("api/live status %d", resp.StatusCode)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		var entries []struct {
			RunID string `json:"run_id"`
		}
		if err := json.Unmarshal(body, &entries); err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if len(entries) == 0 {
			lastErr = fmt.Errorf("no live runs yet")
			time.Sleep(200 * time.Millisecond)
			continue
		}
		return entries[rand.Intn(len(entries))].RunID, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no live runs within 15s")
	}
	return "", lastErr
}

// liveAPIURL converts ws[s]:// or http[s]:// game-server URLs into the
// matching http[s]://host/api/live URL. Same scheme/host (the loadtest
// expects the public proxy in front, where /ws and /api/live share an
// origin), only the path changes.
func liveAPIURL(serverURL string) string {
	u := serverURL
	if strings.HasPrefix(u, "ws://") {
		u = "http://" + strings.TrimPrefix(u, "ws://")
	} else if strings.HasPrefix(u, "wss://") {
		u = "https://" + strings.TrimPrefix(u, "wss://")
	}
	if i := strings.Index(u, "/ws"); i > 0 {
		u = u[:i]
	}
	return strings.TrimRight(u, "/") + "/api/live"
}
