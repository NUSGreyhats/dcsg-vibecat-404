package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"os"
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
	winRoom    = 11
)

const csvPath = "winners.csv"

// Weapon damage/cooldown specs — must match client weapons.ts
type weaponSpec struct {
	Damage     int
	CooldownMs int
}

var weaponSpecs = map[string]weaponSpec{
	"sar21":   {Damage: 1, CooldownMs: 150},
	"maximi":  {Damage: 2, CooldownMs: 220},
	"cs-lr4":  {Damage: 4, CooldownMs: 450},
	"matador": {Damage: 6, CooldownMs: 520},
}

const syncIntervalMs = 1500 // client sends every 1000ms; 500ms buffer for jitter

type heartbeatResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type enemyState struct {
	ID   int     `json:"id"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	HP   int     `json:"hp"`
	Type string  `json:"type"`
}

type gameState struct {
	X              float64      `json:"x"`
	Y              float64      `json:"y"`
	Health         int          `json:"health"`
	CurrentRoom    int          `json:"currentRoom"`
	Kills          int          `json:"kills"`
	Coins          int          `json:"coins"`
	OwnedWeapons   []string     `json:"ownedWeapons"`
	EquippedWeapon string       `json:"equippedWeapon"`
	PlayerName     string       `json:"playerName"`
	Enemies        []enemyState `json:"enemies"`
	Timestamp      float64      `json:"timestamp"`
}

type validationResult struct {
	OK       bool       `json:"ok"`
	Message  string     `json:"message,omitempty"`
	Rollback *gameState `json:"rollback,omitempty"`
}

type playerSession struct {
	mu     sync.Mutex
	last   *gameState
	hasWon bool
}

func cloneEnemies(src []enemyState) []enemyState {
	if len(src) == 0 {
		return nil
	}
	out := make([]enemyState, len(src))
	copy(out, src)
	return out
}

func cloneGameState(src *gameState) gameState {
	cp := *src
	cp.OwnedWeapons = append([]string(nil), src.OwnedWeapons...)
	cp.Enemies = cloneEnemies(src.Enemies)
	return cp
}

func rollbackForPosition(prev, curr *gameState) *gameState {
	rb := cloneGameState(curr)
	rb.X = prev.X
	rb.Y = prev.Y
	return &rb
}

func rollbackForRoomProgression(prev, curr *gameState) *gameState {
	rb := cloneGameState(curr)
	rb.CurrentRoom = prev.CurrentRoom
	rb.X = prev.X
	rb.Y = prev.Y
	rb.Enemies = cloneEnemies(prev.Enemies)
	rb.Kills = prev.Kills
	return &rb
}

func rollbackForEnemyViolation(prev, curr *gameState) *gameState {
	rb := cloneGameState(curr)
	rb.Enemies = cloneEnemies(prev.Enemies)
	rb.Kills = prev.Kills
	return &rb
}

// maxBulletDamage returns the maximum total bullet damage a player could
// deal in one sync interval given their owned weapons.
func maxBulletDamage(owned []string) int {
	best := 0
	for _, wid := range owned {
		ws, ok := weaponSpecs[wid]
		if !ok {
			continue
		}
		shots := syncIntervalMs/ws.CooldownMs + 3
		total := shots * ws.Damage
		if total > best {
			best = total
		}
	}
	if best == 0 {
		best = 100
	}
	return best
}

// validateEnemies checks that enemy HP changes between two snapshots are
// consistent with the player's weapon capabilities.
func validateEnemies(prev, curr *gameState) string {
	currMap := make(map[int]*enemyState, len(curr.Enemies))
	for i := range curr.Enemies {
		currMap[curr.Enemies[i].ID] = &curr.Enemies[i]
	}

	maxDmg := maxBulletDamage(curr.OwnedWeapons)

	// Estimate max kamikaze kills from player HP loss
	playerHpLost := prev.Health - curr.Health
	maxKamikaze := 0
	if playerHpLost > 0 {
		maxKamikaze = playerHpLost
	}
	kamikazeUsed := 0
	totalBulletDmg := 0

	for _, pe := range prev.Enemies {
		ce, alive := currMap[pe.ID]
		if !alive {
			if pe.HP <= maxDmg-totalBulletDmg {
				totalBulletDmg += pe.HP
				continue
			}
			if kamikazeUsed < maxKamikaze {
				kamikazeUsed++
				continue
			}
			return fmt.Sprintf(
				"enemy %d (hp=%d) removed without enough damage (budget=%d remaining)",
				pe.ID, pe.HP, maxDmg-totalBulletDmg)
		}

		if ce.HP > pe.HP {
			return fmt.Sprintf("enemy %d HP increased (%d -> %d)", pe.ID, pe.HP, ce.HP)
		}

		hpDelta := pe.HP - ce.HP
		totalBulletDmg += hpDelta
	}

	if totalBulletDmg > maxDmg {
		return fmt.Sprintf(
			"total bullet damage %d exceeds weapon max %d", totalBulletDmg, maxDmg)
	}

	prevIDs := make(map[int]bool, len(prev.Enemies))
	for _, pe := range prev.Enemies {
		prevIDs[pe.ID] = true
	}
	for _, ce := range curr.Enemies {
		if !prevIDs[ce.ID] {
			return fmt.Sprintf("unknown enemy %d appeared mid-room", ce.ID)
		}
	}

	return ""
}

// validateRoomProgression ensures room advances happen sequentially and only
// after clearing enemies in the current room.
func validateRoomProgression(prev, curr *gameState) string {
	if curr.CurrentRoom < prev.CurrentRoom {
		return fmt.Sprintf("room moved backwards (%d -> %d)", prev.CurrentRoom, curr.CurrentRoom)
	}
	if curr.CurrentRoom == prev.CurrentRoom {
		return ""
	}
	if curr.CurrentRoom > prev.CurrentRoom+1 {
		return fmt.Sprintf("room skip detected (%d -> %d)", prev.CurrentRoom, curr.CurrentRoom)
	}
	if len(prev.Enemies) > 0 {
		return fmt.Sprintf("cannot advance room %d with %d enemies still alive", prev.CurrentRoom, len(prev.Enemies))
	}
	return ""
}

func (s *playerSession) validate(gs *gameState) validationResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.last != nil {
		// Position check
		dx := gs.X - s.last.X
		dy := gs.Y - s.last.Y
		dist := math.Sqrt(dx*dx + dy*dy)

		if dist > maxDist {
			rollback := rollbackForPosition(s.last, gs)
			return validationResult{
				OK:       false,
				Message:  "position delta exceeds half-map threshold",
				Rollback: rollback,
			}
		}

		// Room progression checks.
		if msg := validateRoomProgression(s.last, gs); msg != "" {
			rollback := rollbackForRoomProgression(s.last, gs)
			return validationResult{
				OK:       false,
				Message:  msg,
				Rollback: rollback,
			}
		}

		// Enemy checks within a room.
		if gs.CurrentRoom == s.last.CurrentRoom {
			if msg := validateEnemies(s.last, gs); msg != "" {
				rollback := rollbackForEnemyViolation(s.last, gs)
				return validationResult{
					OK:       false,
					Message:  msg,
					Rollback: rollback,
				}
			}
		}
	}

	s.last = gs
	return validationResult{OK: true}
}

// --- leaderboard persistence ---

type winner struct {
	Name string
	Time string
}

var (
	leaderMu sync.Mutex
	leaders  []winner
)

func loadLeaderboard() {
	f, err := os.Open(csvPath)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Printf("warning: could not open %s: %v", csvPath, err)
		return
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		log.Printf("warning: could not read %s: %v", csvPath, err)
		return
	}

	for _, rec := range records {
		if len(rec) >= 2 {
			leaders = append(leaders, winner{Name: rec[0], Time: rec[1]})
		}
	}
	log.Printf("loaded %d leaderboard entries from %s", len(leaders), csvPath)
}

func appendWinner(name string) {
	leaderMu.Lock()
	defer leaderMu.Unlock()

	ts := time.Now().UTC().Format(time.RFC3339)
	w := winner{Name: name, Time: ts}
	leaders = append(leaders, w)

	f, err := os.OpenFile(csvPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("error opening %s for append: %v", csvPath, err)
		return
	}
	defer f.Close()

	cw := csv.NewWriter(f)
	_ = cw.Write([]string{w.Name, w.Time})
	cw.Flush()
}

func getLeaders() []winner {
	leaderMu.Lock()
	defer leaderMu.Unlock()
	out := make([]winner, len(leaders))
	copy(out, leaders)
	return out
}

// --- websocket handler ---

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
			continue
		}

		if !session.hasWon && gs.CurrentRoom >= winRoom {
			session.hasWon = true
			name := gs.PlayerName
			if name == "" {
				name = "Anonymous"
			}
			log.Printf("WINNER: %s from %s (room=%d kills=%d)",
				name, r.RemoteAddr, gs.CurrentRoom, gs.Kills)
			appendWinner(name)
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
		log.Printf("[debug] %s: room=%d pos=(%.1f, %.1f) hp=%d kills=%d coins=%d name=%s",
			addr, gs.CurrentRoom, gs.X, gs.Y, gs.Health, gs.Kills, gs.Coins, gs.PlayerName)
	}
}

// --- leaderboard page ---

var leaderboardTmpl = template.Must(template.New("lb").Funcs(template.FuncMap{
	"inc": func(i int) int { return i + 1 },
}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Leaderboard</title>
<style>
  *{margin:0;padding:0;box-sizing:border-box}
  body{background:#0d1117;color:#c9d1d9;font-family:'Segoe UI',Arial,sans-serif;padding:40px 20px}
  h1{text-align:center;font-size:2.4rem;margin-bottom:32px;color:#58a6ff}
  table{margin:0 auto;border-collapse:collapse;min-width:400px;max-width:700px;width:100%}
  th,td{padding:12px 20px;text-align:left}
  th{background:#161b22;color:#8b949e;font-size:.85rem;text-transform:uppercase;letter-spacing:.05em;border-bottom:2px solid #30363d}
  tr:nth-child(even) td{background:#161b22}
  td{border-bottom:1px solid #21262d;font-size:1rem}
  tr:first-child td{color:#ffd700;font-weight:600}
  .empty{text-align:center;color:#8b949e;padding:40px;font-size:1.1rem}
  .rank{color:#8b949e;font-weight:600}
</style>
</head>
<body>
<h1>Leaderboard</h1>
{{if .}}
<table>
<thead><tr><th>#</th><th>Player</th><th>Time (UTC)</th></tr></thead>
<tbody>
{{range $i, $w := .}}
<tr><td class="rank">{{inc $i}}</td><td>{{$w.Name}}</td><td>{{$w.Time}}</td></tr>
{{end}}
</tbody>
</table>
{{else}}
<p class="empty">No winners yet. Be the first!</p>
{{end}}
</body>
</html>
`))

func serveLeaderboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := getLeaders()
	if err := leaderboardTmpl.Execute(w, data); err != nil {
		log.Printf("leaderboard template error: %v", err)
	}
}

// --- heartbeat ---

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

	loadLeaderboard()

	mux := http.NewServeMux()

	mux.HandleFunc("/ws", handleGameWS)

	mux.HandleFunc("/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/leaderboard" {
			http.NotFound(w, r)
			return
		}
		serveLeaderboard(w, r)
	})

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
	fmt.Printf("listening on http://%s/\n", addr)
	fmt.Printf("leaderboard: http://%s/leaderboard\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
