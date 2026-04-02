package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

var debug = flag.Bool("debug", false, "enable debug logging (prints player state every 5s)")

const welcome = "Welcome to the hidden world where the flag is grey{h1dd3n_3nDP01N7}"

const (
	roomWidth  = 1024
	roomHeight = 768
	maxDist    = math.Sqrt2 * float64(max(roomWidth, roomHeight)) / 2
)

type heartbeatResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type gameState struct {
	X              float64  `json:"x"`
	Y              float64  `json:"y"`
	Health         int      `json:"health"`
	CurrentRoom    int      `json:"currentRoom"`
	Kills          int      `json:"kills"`
	Coins          int      `json:"coins"`
	OwnedWeapons   []string `json:"ownedWeapons"`
	EquippedWeapon string   `json:"equippedWeapon"`
	Timestamp      float64  `json:"timestamp"`
}

type validationResult struct {
	OK       bool       `json:"ok"`
	Message  string     `json:"message,omitempty"`
	Rollback *gameState `json:"rollback,omitempty"`
}

type playerSession struct {
	mu   sync.Mutex
	last *gameState
}

func (s *playerSession) validate(gs *gameState) validationResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.last != nil {
		dx := gs.X - s.last.X
		dy := gs.Y - s.last.Y
		dist := math.Sqrt(dx*dx + dy*dy)

		if dist > maxDist {
			rollback := *s.last
			return validationResult{
				OK:       false,
				Message:  "position delta exceeds half-map threshold",
				Rollback: &rollback,
			}
		}
	}

	s.last = gs
	return validationResult{OK: true}
}

func handleGameWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("ws accept: %v", err)
		return
	}
	defer conn.CloseNow()

	log.Printf("player connected from %s", r.RemoteAddr)
	session := &playerSession{}

	if *debug {
		go session.debugTicker(r.RemoteAddr)
	}

	for {
		_, data, err := conn.Read(context.Background())
		if err != nil {
			log.Printf("ws read (%s): %v", r.RemoteAddr, err)
			return
		}

		var gs gameState
		if err := json.Unmarshal(data, &gs); err != nil {
			log.Printf("bad payload from %s: %v", r.RemoteAddr, err)
			continue
		}

		res := session.validate(&gs)
		out, _ := json.Marshal(res)
		if err := conn.Write(context.Background(), websocket.MessageText, out); err != nil {
			log.Printf("ws write (%s): %v", r.RemoteAddr, err)
			return
		}

		if !res.OK {
			log.Printf("VIOLATION %s room=%d pos=(%.0f,%.0f): %s",
				r.RemoteAddr, gs.CurrentRoom, gs.X, gs.Y, res.Message)
		}
	}
}

func (s *playerSession) debugTicker(addr string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		gs := s.last
		s.mu.Unlock()

		if gs == nil {
			log.Printf("[debug] %s: no state received yet", addr)
			continue
		}
		log.Printf("[debug] %s: room=%d pos=(%.1f, %.1f) hp=%d kills=%d coins=%d",
			addr, gs.CurrentRoom, gs.X, gs.Y, gs.Health, gs.Kills, gs.Coins)
	}
}

func serveHeartbeatJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte(`{"error":"method not allowed"}` + "\n"))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	resp := heartbeatResponse{Status: "online", Message: "heartbeat"}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("encode heartbeat json: %v", err)
	}
}

func main() {
	flag.Parse()

	mux := http.NewServeMux()

	mux.HandleFunc("/ws", handleGameWS)

	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" {
			http.NotFound(w, r)
			return
		}
		serveHeartbeatJSON(w, r)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/" {
			http.NotFound(w, r)
			return
		}
		serveHeartbeatJSON(w, r)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(welcome))
	})

	addr := "127.0.0.1:8888"
	log.Printf("listening on http://%s/", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
