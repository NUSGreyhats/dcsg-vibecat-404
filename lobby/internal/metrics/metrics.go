// Package metrics owns the lobby's Prometheus instrumentation. Each
// metric is a typed package-level variable so call sites read as
// `metrics.HTTPRequests.WithLabelValues(...)` rather than touching the
// registry directly.
//
// The `service` label is intentionally omitted from every metric — the
// scrape job already labels samples by service (job=lobby vs
// job=game-server in obs/victoria-metrics/scrape.yml). Adding it again
// here would just inflate cardinality without telling the dashboards
// anything new.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// httpBuckets are tighter than client_golang's defaults (which span
// 5 ms to 10 s in 11 buckets). Lobby endpoints either return in a few
// ms or are blocked on bcrypt (~100–500 ms). Five buckets cover that
// range; total samples per series stay bounded for the 30-route × 9-
// status × 4-method matrix.
var httpBuckets = []float64{0.005, 0.025, 0.1, 0.5, 2.5}

var (
	HTTPRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Lobby HTTP requests by method, normalised route, and status code.",
		},
		[]string{"method", "route", "status"},
	)

	HTTPDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Lobby HTTP request duration in seconds.",
			Buckets: httpBuckets,
		},
		[]string{"method", "route", "status"},
	)

	// QueueDepth is the count of rows in queue_entries. Polled once per
	// few seconds; cheap because the table is tiny.
	QueueDepth = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "queue_depth",
			Help: "Lobby FIFO queue depth (admin-bounded by lobby_admission_cap).",
		},
	)

	// QueueDwellTime is observed at the moment the advancer admits a
	// player — value is `now − joined_at` in seconds. Buckets cover a
	// few seconds (light queue) up to the 10-min absolute MaxRunDuration.
	QueueDwellTime = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "queue_dwell_time_seconds",
			Help:    "Time a player waited in the queue before admission.",
			Buckets: []float64{1, 5, 30, 120, 300, 600, 1800},
		},
	)

	// LobbyAdmissionCap mirrors the admin-tunable max_concurrent setting.
	// Distinct from concurrent_cap (game-server's MAX_CONCURRENT env): the
	// lobby's cap can be changed at runtime via the admin panel.
	LobbyAdmissionCap = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "lobby_admission_cap",
			Help: "Current lobby admission cap (admin-tunable).",
		},
	)

	// Errors is intentionally low-cardinality. The `kind` label is a
	// hand-curated enum (admin_auth, queue_write, db_read, …); never
	// raw error strings.
	Errors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "errors_total",
			Help: "Lobby internal errors by kind (curated enum, not raw error strings).",
		},
		[]string{"kind"},
	)
)

// registry holds every metric this package exposes. Using a private
// registry (rather than prometheus.DefaultRegisterer) keeps the lobby's
// /metrics output free of stray globals from indirectly imported libs
// that may have called MustRegister on the default.
var registry = prometheus.NewRegistry()

func init() {
	registry.MustRegister(
		HTTPRequests,
		HTTPDuration,
		QueueDepth,
		QueueDwellTime,
		LobbyAdmissionCap,
		Errors,
		// process_* (RSS, open_fds, cpu_seconds, …) and go_* (goroutines,
		// memstats) come for free from these collectors. Spec inventory
		// names process_resident_memory_bytes / process_open_fds /
		// go_goroutines explicitly — they all surface from here.
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector(),
	)
}

// Handler returns the http.Handler that serves /metrics. Wired in
// lobby/cmd/lobby/main.go alongside the rest of the routes.
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	})
}
