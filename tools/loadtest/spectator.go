package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"
)

// runSpectator opens a /ws/spectate connection for the given player_id and
// drains state/runtime frames until the run ends or ctx fires. Spectators
// are read-only — any frame back to the server triggers a 1008 close, so
// we simply pump reads.
func runSpectator(ctx context.Context, cfg config, m *metrics, playerID string) {
	m.connAttempts.Add(1)
	u := cfg.serverURL + "/ws/spectate?player_id=" + url.QueryEscape(playerID)

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
