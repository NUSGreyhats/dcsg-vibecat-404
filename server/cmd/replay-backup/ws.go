package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/greyhats/defcon-game/pkg/replayauth"
	"github.com/greyhats/defcon-game/server/internal/game"
	"github.com/greyhats/defcon-game/server/internal/protocol"
	"github.com/greyhats/defcon-game/server/internal/replay"
)

// wsReplayHandler bridges /ws/replay to the in-memory store. Slim
// version of server/cmd/server/main.go's handleReplayWS:
//   - ticket validation against the in-process secret (same scheme,
//     so the existing Phaser client works unmodified)
//   - run lookup via the in-memory store instead of game.db
//   - no caps / per-IP rate limit / metrics (single operator, local)
//   - no keepalive ping (local TCP, no idle NAT to worry about)
//
// Reader / writer / player goroutine shape mirrors the production
// handler verbatim so the WS protocol behaves identically from the
// client's perspective.
//
// `parent` is the server-lifetime context, NOT the per-request context.
// Crucially: r.Context() is cancelled the moment this handler returns
// (which happens immediately after Accept + goroutine spawn), so basing
// connCtx on it would tear down the WS goroutines before they emit a
// single frame. The prod handler in server/cmd/server/main.go threads a
// long-lived parent ctx through for exactly this reason.
func wsReplayHandler(parent context.Context, st *store, secret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		replayID := r.URL.Query().Get("replay_id")
		ticket := r.URL.Query().Get("ticket")
		if !uuidPattern.MatchString(replayID) {
			http.Error(w, "invalid replay_id", http.StatusBadRequest)
			return
		}
		if err := replayauth.Verify(secret, ticket, replayID, time.Now()); err != nil {
			w.Header().Set("WWW-Authenticate", "Replay")
			if errors.Is(err, replayauth.ErrExpired) {
				http.Error(w, "ticket expired", http.StatusUnauthorized)
			} else {
				http.Error(w, "invalid ticket", http.StatusUnauthorized)
			}
			return
		}
		run, ok := st.get(replayID)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			log.Printf("replay-backup: ws accept: %v", err)
			return
		}

		connCtx, cancel := context.WithCancel(parent)
		player := replay.New(run.PlayerID, run.DisplayName, run.Seed, run.TotalMs, run.Records)

		// Reader: accept C2S_REPLAY_CONTROL only.
		go func() {
			defer cancel()
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
				}
			}
		}()

		// Player: drives the engine; closes player.OutCh on exit.
		go player.Run(connCtx)

		// Writer: drain player.OutCh → WS. Owns the WS close.
		go func() {
			defer cancel()
			defer wsConn.Close(websocket.StatusNormalClosure, "bye")
			for {
				select {
				case <-connCtx.Done():
					return
				case env, ok := <-player.OutCh:
					if !ok {
						// Match prod's 500ms grace flush so the final
						// RUN_COMPLETE has time to land before close.
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
					if err := wsConn.Write(connCtx, websocket.MessageText, raw); err != nil {
						return
					}
				}
			}
		}()
	}
}

// writeReplayError pushes a single S2C_ERROR envelope before the
// caller closes the WS. Best-effort.
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
