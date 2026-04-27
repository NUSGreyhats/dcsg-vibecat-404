// Package metrics owns the game-server's Prometheus instrumentation.
// Each metric is a typed package-level variable so call sites read as
// `metrics.WSMessages.WithLabelValues(...)` rather than touching the
// registry directly.
//
// The `service` label is intentionally omitted from every metric — the
// scrape job already labels samples by service (job=lobby vs
// job=game-server in obs/victoria-metrics/scrape.yml).
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/greyhats/defcon-game/server/internal/protocol"
)

// Tick budget is 50 ms (20 Hz). Buckets focus on that target with
// headroom up to 100 ms — anything beyond that is already a problem.
var tickBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1}

// Per-packet handler time. Most C2S handlers are sub-ms (input updates,
// shoot); a few do more work (interact, enter_door). 5 ms ceiling
// keeps the tick coherent.
var wsHandlerBuckets = []float64{0.0001, 0.0005, 0.001, 0.005}

// Run duration buckets cover the realistic range for a 6-room run
// bounded by MaxRunDuration = 10 minutes.
var runDurationBuckets = []float64{30, 60, 120, 240, 480, 900}

// Per-room split buckets: combat rooms clear in seconds, puzzle/boss
// rooms take longer. 2 s lower bound catches "rushed through", 180 s
// upper bound is well past any honest single-room time.
var roomSplitBuckets = []float64{2, 5, 10, 20, 45, 90, 180}

var (
	GameTickDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "game_tick_duration_seconds",
			Help:    "Game-server stepTick duration. Target 50 ms (20 Hz).",
			Buckets: tickBuckets,
		},
	)

	WSHandlerDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ws_message_handler_duration_seconds",
			Help:    "Per-C2S-packet dispatch duration.",
			Buckets: wsHandlerBuckets,
		},
		[]string{"packet_type"},
	)

	WSMessages = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_messages_total",
			Help: "Game-server WebSocket messages by direction (c2s|s2c) and packet type.",
		},
		[]string{"direction", "packet_type"},
	)

	WSConnsActive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ws_connections_active",
			Help: "Active /ws connections (post-accept, pre-close).",
		},
	)

	SlotInUse = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "slot_semaphore_in_use",
			Help: "Active /ws slots held against MAX_CONCURRENT (= concurrent_cap − free).",
		},
	)

	ConcurrentCap = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "concurrent_cap",
			Help: "Game-server MAX_CONCURRENT (set once at startup; restart-only).",
		},
	)

	SpectatorConnsActive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "spectator_connections_active",
			Help: "Active /ws/spectate connections.",
		},
	)

	ReplayViewersActive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "replay_viewers_active",
			Help: "Active /ws/replay connections.",
		},
	)

	ActivePlayers = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "active_players",
			Help: "Players currently in a run (handshake complete, tickLoop running).",
		},
	)

	RunsCompleted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "runs_completed_total",
			Help: "Runs that reached a terminal state. Outcome: completed (boss kill) | failed (death/idle/cap).",
		},
		[]string{"outcome"},
	)

	RunDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "run_duration_seconds",
			Help:    "Distribution of run wall-clock durations at finishRun.",
			Buckets: runDurationBuckets,
		},
		[]string{"outcome"},
	)

	// AuthModeInfo is a Prometheus info metric — constant value 1 with
	// the running mode in a label. Exactly one series is set at startup;
	// changes require a restart, which matches the env-var-only contract.
	AuthModeInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "auth_mode_info",
			Help: "Game-server auth mode (info-metric: value is always 1).",
		},
		[]string{"mode"},
	)

	Errors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "errors_total",
			Help: "Game-server internal errors by curated kind enum.",
		},
		[]string{"kind"},
	)

	// RoomSplitDuration is observed at every room transition. The room
	// label is the room index just being left (0..FinalRoom).
	RoomSplitDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "room_split_duration_seconds",
			Help:    "Time spent in each room between transitions (room=index just left).",
			Buckets: roomSplitBuckets,
		},
		[]string{"room"},
	)

	// WeaponShots counts every accepted shot (post cooldown + ammo
	// gates) by weapon name. Shotgun pellets are one shot, not three.
	WeaponShots = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "weapon_shots_total",
			Help: "Accepted player shots by weapon name (one shot, not per-pellet).",
		},
		[]string{"weapon"},
	)
)

var registry = prometheus.NewRegistry()

func init() {
	registry.MustRegister(
		GameTickDuration,
		WSHandlerDuration,
		WSMessages,
		WSConnsActive,
		SlotInUse,
		ConcurrentCap,
		SpectatorConnsActive,
		ReplayViewersActive,
		ActivePlayers,
		RunsCompleted,
		RunDuration,
		AuthModeInfo,
		Errors,
		RoomSplitDuration,
		WeaponShots,
		// process_resident_memory_bytes / process_open_fds / go_goroutines
		// surface from these.
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector(),
	)
}

func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	})
}

// PacketTypeName maps protocol envelope type bytes to stable, low-
// cardinality label values. Unknown bytes return "unknown" rather than
// the raw uint8 — keeps WSMessages bounded even under malformed input.
func PacketTypeName(t uint8) string {
	switch t {
	case protocol.C2SAuth:
		return "auth"
	case protocol.C2SStartRun:
		return "start_run"
	case protocol.C2SInput:
		return "input"
	case protocol.C2SShoot:
		return "shoot"
	case protocol.C2SSwitchWeapon:
		return "switch_weapon"
	case protocol.C2SInteract:
		return "interact"
	case protocol.C2SEnterDoor:
		return "enter_door"
	case protocol.C2SShopPurchase:
		return "shop_purchase"
	case protocol.C2SReplayControl:
		return "replay_control"
	case protocol.S2CAuthOk:
		return "auth_ok"
	case protocol.S2CRunStarted:
		return "run_started"
	case protocol.S2CState:
		return "state"
	case protocol.S2CRoomLoad:
		return "room_load"
	case protocol.S2CRunComplete:
		return "run_complete"
	case protocol.S2CLeaderboard:
		return "leaderboard"
	case protocol.S2CError:
		return "error"
	case protocol.S2CDoorUnlocked:
		return "door_unlocked"
	case protocol.S2CEntityDied:
		return "entity_died"
	}
	return "unknown"
}
