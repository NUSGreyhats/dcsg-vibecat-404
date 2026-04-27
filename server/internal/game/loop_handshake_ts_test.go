package game

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/greyhats/defcon-game/server/internal/protocol"
)

// awaitAuth and awaitStartRun must enforce the per-packet ±2s ts window
// (TsValidationWindow) — the bug behind issue #46. Without this check, a
// skewed-clock client would pass the handshake, enter the Game scene,
// and then have every C2S_INPUT silently rejected by dispatch, leaving
// the player frozen with no surfaced error.

func newHandshakeTestEngine() (*Engine, chan []byte, chan protocol.S2CEnvelope) {
	in := make(chan []byte, 4)
	out := make(chan protocol.S2CEnvelope, 16)
	eng := NewEngine(in, out, nil, nil)
	return eng, in, out
}

func encodeC2S(t *testing.T, typ uint8, ts int64, data any) []byte {
	t.Helper()
	payload, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	raw, err := json.Marshal(protocol.Envelope{
		Type: typ,
		Seq:  1,
		Ts:   ts,
		Data: payload,
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return raw
}

func drainErr(out chan protocol.S2CEnvelope) *protocol.ErrorData {
	for {
		select {
		case env := <-out:
			if env.Type == protocol.S2CError {
				if d, ok := env.Data.(protocol.ErrorData); ok {
					return &d
				}
			}
		default:
			return nil
		}
	}
}

func TestAwaitAuthRejectsSkewedTs(t *testing.T) {
	eng, in, out := newHandshakeTestEngine()
	// Client ts in the past → client clock is "behind" the server.
	skewed := time.Now().Add(-1 * time.Hour).UnixMilli()
	in <- encodeC2S(t, protocol.C2SAuth, skewed, protocol.AuthPacket{PlayerID: "p"})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if eng.awaitAuth(ctx) {
		t.Fatal("expected awaitAuth to reject a skewed-ts AUTH and return false")
	}
	errData := drainErr(out)
	if errData == nil {
		t.Fatal("expected S2C_ERROR on out channel")
	}
	if errData.Code != "ts_out_of_window" {
		t.Fatalf("expected code=ts_out_of_window, got %q", errData.Code)
	}
	if !strings.Contains(errData.Message, "behind server") {
		t.Fatalf("expected 'behind server' direction in message, got %q", errData.Message)
	}
}

func TestAwaitAuthMessageDirectionWhenClientAhead(t *testing.T) {
	// Client ts in the future → "ahead of" server. The message must use
	// an absolute offset and the correct direction word — not a raw
	// negative diff like "-3600000 ms".
	eng, in, out := newHandshakeTestEngine()
	skewed := time.Now().Add(1 * time.Hour).UnixMilli()
	in <- encodeC2S(t, protocol.C2SAuth, skewed, protocol.AuthPacket{PlayerID: "p"})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if eng.awaitAuth(ctx) {
		t.Fatal("expected awaitAuth to reject a skewed-ts AUTH and return false")
	}
	errData := drainErr(out)
	if errData == nil || errData.Code != "ts_out_of_window" {
		t.Fatalf("expected code=ts_out_of_window, got %+v", errData)
	}
	if !strings.Contains(errData.Message, "ahead of server") {
		t.Fatalf("expected 'ahead of server' direction in message, got %q", errData.Message)
	}
	if strings.Contains(errData.Message, "-") {
		t.Fatalf("message must not contain a negative diff; got %q", errData.Message)
	}
}

func TestAwaitAuthAcceptsTsZeroForReplay(t *testing.T) {
	// Replay's feedAuth uses Ts=0 (server/internal/replay/player.go:306).
	// The actual replay driver also sets ReplayMode=true before
	// feedAuth/feedStartRun, so either short-circuit alone is sufficient
	// — but this test pins the Ts==0 path independently in case the
	// ReplayMode ordering changes.
	eng, in, out := newHandshakeTestEngine()
	in <- encodeC2S(t, protocol.C2SAuth, 0, protocol.AuthPacket{PlayerID: "p"})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if !eng.awaitAuth(ctx) {
		errData := drainErr(out)
		t.Fatalf("expected awaitAuth to accept Ts=0; got error %+v", errData)
	}
}

func TestAwaitAuthAcceptsValidTs(t *testing.T) {
	eng, in, out := newHandshakeTestEngine()
	in <- encodeC2S(t, protocol.C2SAuth, time.Now().UnixMilli(), protocol.AuthPacket{PlayerID: "p"})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if !eng.awaitAuth(ctx) {
		errData := drainErr(out)
		t.Fatalf("expected awaitAuth to accept valid ts; got error %+v", errData)
	}
}

func TestAwaitStartRunRejectsSkewedTs(t *testing.T) {
	eng, in, out := newHandshakeTestEngine()
	eng.PlayerID = "p"
	eng.DisplayName = "p"
	skewed := time.Now().Add(5 * time.Second).UnixMilli()
	in <- encodeC2S(t, protocol.C2SStartRun, skewed,
		protocol.StartRunPacket{StartTime: time.Now().UnixMilli()})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if eng.awaitStartRun(ctx) {
		t.Fatal("expected awaitStartRun to reject a skewed-ts START_RUN and return false")
	}
	errData := drainErr(out)
	if errData == nil || errData.Code != "ts_out_of_window" {
		t.Fatalf("expected code=ts_out_of_window, got %+v", errData)
	}
}

func TestReplayModeSkipsHandshakeTsCheck(t *testing.T) {
	// In ReplayMode, validateTsWindow must short-circuit so an old
	// recording's ts (potentially many minutes from server now) is
	// accepted. Without this, replay would fail to start.
	eng, in, out := newHandshakeTestEngine()
	eng.ReplayMode = true
	stale := time.Now().Add(-2 * time.Hour).UnixMilli()
	in <- encodeC2S(t, protocol.C2SAuth, stale, protocol.AuthPacket{PlayerID: "p"})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if !eng.awaitAuth(ctx) {
		errData := drainErr(out)
		t.Fatalf("expected awaitAuth in ReplayMode to accept stale ts; got error %+v", errData)
	}
}
