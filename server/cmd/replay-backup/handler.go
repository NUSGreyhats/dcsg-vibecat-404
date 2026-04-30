package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/greyhats/defcon-game/pkg/replayauth"
	"github.com/greyhats/defcon-game/server/internal/game"
)

//go:embed upload.html
var uploadHTML []byte

// replayDownload mirrors the JSON shape the lobby's
// /admin/replay/:id endpoint emits (lobby/internal/leaderboard.Replay).
// Defined locally because that package lives behind an internal/
// barrier — and the JSON contract is what we actually depend on.
type replayDownload struct {
	ReplayID    string `json:"replay_id"`
	PlayerID    string `json:"player_id"`
	DisplayName string `json:"display_name"`
	TotalMs     int64  `json:"total_ms"`
	Seed        int64  `json:"seed"`
	InputLog    []byte `json:"input_log"`
}

// replayMetaResponse mirrors lobby/internal/leaderboard.ReplayMeta —
// the shape the Phaser viewer expects from /admin/replay/:id/meta.
type replayMetaResponse struct {
	ReplayID    string        `json:"replay_id"`
	DisplayName string        `json:"display_name"`
	TotalMs     int64         `json:"total_ms"`
	Seed        int64         `json:"seed"`
	Splits      []replaySplit `json:"splits"`
	SubmittedAt string        `json:"submitted_at"`
}

type replaySplit struct {
	RoomIndex uint8 `json:"room_index"`
	TimeMs    int64 `json:"time_ms"`
	Kills     uint8 `json:"kills"`
}

// uuidPattern matches the canonical UUID-v4 hex shape with dashes.
// Loosely matched (no version-bit assertions) — same shape the prod
// game server uses (server/cmd/server/main.go:552).
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// maxUploadBytes caps the multipart upload size. Real recorded runs
// run a few hundred KB to a few MB. 64 MB is comfortable headroom
// without letting a stray file fill the operator's RAM.
const maxUploadBytes = 64 << 20

// registerRoutes wires the handler set onto mux. Kept separate from
// main so smoke tests can mount the routes onto a httptest server.
// `parent` is the server-lifetime context — passed through to
// wsReplayHandler so its goroutines outlive the HTTP request that
// upgraded the WebSocket.
func registerRoutes(parent context.Context, mux *http.ServeMux, st *store, secret []byte, clientDist string) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(uploadHTML)
	})
	mux.HandleFunc("/upload", uploadHandler(st))
	mux.HandleFunc("/admin/replay/", adminReplayDispatch(st, secret, clientDist))
	mux.HandleFunc("/ws/replay", wsReplayHandler(parent, st, secret))
	// Vite's prod build emits hashed asset filenames into /assets/* and
	// also drops top-level entry chunks like /replay-<hash>.js. Serve
	// everything from the dist directory so relative imports resolve
	// the same way they do under Caddy in prod.
	fileServer := http.FileServer(http.Dir(clientDist))
	mux.Handle("/assets/", fileServer)
	mux.Handle("/favicon.ico", fileServer)
	// Fallthrough for any *.js / *.css the bundle references at the
	// root. Anything not present in dist returns 404 from FileServer.
	mux.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/static/", fileServer).ServeHTTP(w, r)
	})
}

// uploadHandler parses the uploaded file as a `lobby/.../leaderboard.Replay`
// JSON object, decodes the base64-encoded input_log, unmarshals it as
// []game.InputRecord (the shape replay.Player expects), stores the run
// in memory, and 302s to the viewer.
func uploadHandler(st *store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
		if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
			http.Error(w, fmt.Sprintf("parse multipart: %v", err), http.StatusBadRequest)
			return
		}
		file, _, err := r.FormFile("run")
		if err != nil {
			http.Error(w, fmt.Sprintf("read upload field 'run': %v", err), http.StatusBadRequest)
			return
		}
		defer file.Close()

		var rep replayDownload
		if err := json.NewDecoder(file).Decode(&rep); err != nil {
			http.Error(w, fmt.Sprintf("decode JSON: %v", err), http.StatusBadRequest)
			return
		}
		if !uuidPattern.MatchString(rep.ReplayID) {
			http.Error(w, "replay_id must be a UUID", http.StatusBadRequest)
			return
		}
		if len(rep.InputLog) == 0 {
			http.Error(w, "input_log is empty", http.StatusBadRequest)
			return
		}

		// Sanity-check the input_log bytes before storing. Catches a
		// truncated download or wrong-file upload here rather than at
		// WS-upgrade time when the operator is staring at a Phaser
		// loading screen.
		var records []game.InputRecord
		if err := json.Unmarshal(rep.InputLog, &records); err != nil {
			http.Error(w, fmt.Sprintf("input_log is not a valid []InputRecord: %v", err), http.StatusBadRequest)
			return
		}

		st.put(&storedRun{
			ReplayID:    rep.ReplayID,
			PlayerID:    rep.PlayerID,
			DisplayName: rep.DisplayName,
			TotalMs:     rep.TotalMs,
			Seed:        rep.Seed,
			Records:     records,
			UploadedAt:  time.Now(),
		})

		http.Redirect(w, r, "/admin/replay/"+rep.ReplayID+"/view", http.StatusSeeOther)
	}
}

// adminReplayDispatch routes the three /admin/replay/<id>/... paths the
// existing replay client expects: /view (HTML), /meta (JSON), /ticket
// (POST → JSON). Bare /admin/replay/<id> is unused by the viewer (it's
// the JSON download path on the lobby) so we leave it as 404 here —
// the operator already has the file locally.
func adminReplayDispatch(st *store, secret []byte, clientDist string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/admin/replay/")
		switch {
		case strings.HasSuffix(rest, "/view"):
			id := strings.TrimSuffix(rest, "/view")
			if !uuidPattern.MatchString(id) {
				http.Error(w, "invalid replay_id", http.StatusBadRequest)
				return
			}
			serveReplayHTML(w, r, clientDist)
		case strings.HasSuffix(rest, "/meta"):
			id := strings.TrimSuffix(rest, "/meta")
			if !uuidPattern.MatchString(id) {
				http.Error(w, "invalid replay_id", http.StatusBadRequest)
				return
			}
			metaHandler(w, r, st, id)
		case strings.HasSuffix(rest, "/ticket"):
			id := strings.TrimSuffix(rest, "/ticket")
			if !uuidPattern.MatchString(id) {
				http.Error(w, "invalid replay_id", http.StatusBadRequest)
				return
			}
			ticketHandler(w, r, st, secret, id)
		default:
			http.NotFound(w, r)
		}
	}
}

// serveReplayHTML mirrors what client/Caddyfile:78-83 does in prod:
// every URL of shape /admin/replay/<id>/view is rewritten to the
// static replay.html. The bundle's <base href="/"> means asset
// imports resolve to root regardless of the requesting URL.
func serveReplayHTML(w http.ResponseWriter, r *http.Request, clientDist string) {
	htmlPath := filepath.Join(clientDist, "replay.html")
	f, err := os.Open(htmlPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "replay.html not found in client-dist — did you run `npm run build` in client/?", http.StatusInternalServerError)
			return
		}
		http.Error(w, fmt.Sprintf("open replay.html: %v", err), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		http.Error(w, fmt.Sprintf("stat replay.html: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "replay.html", stat.ModTime(), f)
}

// metaHandler returns the same JSON shape the lobby's
// /admin/replay/:id/meta returns. Splits and submitted_at aren't in
// the JSON download format (it omits splits_json) — synthesize empty
// splits and the upload time. The viewer end-screen will show no
// per-room splits, which is acceptable for an operator triage tool.
func metaHandler(w http.ResponseWriter, r *http.Request, st *store, id string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	run, ok := st.get(id)
	if !ok {
		http.Error(w, "replay not uploaded — POST it to /upload first", http.StatusNotFound)
		return
	}
	resp := replayMetaResponse{
		ReplayID:    run.ReplayID,
		DisplayName: run.DisplayName,
		TotalMs:     run.TotalMs,
		Seed:        run.Seed,
		Splits:      []replaySplit{},
		SubmittedAt: run.UploadedAt.UTC().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ticketHandler mints a ticket the same way the lobby does: HMAC over
// "replay:<id>:<exp>" with the local secret. The /ws/replay handler
// validates against the same secret in the same process.
func ticketHandler(w http.ResponseWriter, r *http.Request, st *store, secret []byte, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := st.get(id); !ok {
		http.Error(w, "replay not uploaded", http.StatusNotFound)
		return
	}
	exp := time.Now().Add(replayauth.TTL)
	ticket := replayauth.Sign(secret, id, exp)
	resp := struct {
		Ticket    string `json:"ticket"`
		ExpiresAt int64  `json:"expires_at"`
	}{
		Ticket:    ticket,
		ExpiresAt: exp.UnixMilli(),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
