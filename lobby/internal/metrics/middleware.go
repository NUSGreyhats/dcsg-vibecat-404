package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Middleware records http_requests_total and http_request_duration_seconds
// for every request that reaches the wrapped mux, except /metrics itself
// (avoid self-counting on every scrape).
//
// The `route` label is normalised by NormaliseRoute below — raw paths
// like /admin/replay/{32-hex} would explode cardinality.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		elapsed := time.Since(start).Seconds()

		method := normaliseMethod(r.Method)
		route := NormaliseRoute(r.URL.Path)
		status := strconv.Itoa(rec.status)
		HTTPRequests.WithLabelValues(method, route, status).Inc()
		HTTPDuration.WithLabelValues(method, route, status).Observe(elapsed)
	})
}

// statusRecorder captures the status code so the middleware can label it.
// Default to 200 — handlers that write a body without calling WriteHeader
// implicitly produce a 200.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// Write tracks the implicit-200 case: if a handler calls Write without
// first calling WriteHeader, Go's stdlib silently sends a 200. Without
// this method, a later (no-op) WriteHeader(500) call would still update
// our recorded status and mislabel the metric. Marking wroteHeader here
// freezes status at its default (200) once any byte has been written.
func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(b)
}

// normaliseMethod bounds the `method` label to known HTTP verbs. Without
// this, a scanner sending arbitrary methods (`FOO /admin/...`) would mint
// new series indefinitely.
func normaliseMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost,
		http.MethodPut, http.MethodPatch, http.MethodDelete,
		http.MethodOptions:
		return m
	}
	return "other"
}

// NormaliseRoute collapses parameterised paths to a stable label value
// (e.g. /admin/replay/abc123 → /admin/replay/:id). Unknown paths return
// "other" rather than the raw path so an attacker scanning random URLs
// can't blow up our series count.
func NormaliseRoute(path string) string {
	switch path {
	case
		// Identity (mounted by pkg/identity.Service.Register).
		"/api/register", "/api/verify", "/api/login",
		// Lobby surfaces.
		"/api/health", "/api/config", "/api/leaderboard", "/api/live",
		"/api/queue/join",
		"/leaderboard", "/spectate", "/source.tar.gz",
		// Admin (gated by adminauth.Wrap, but routing is here).
		"/admin", "/admin/",
		"/admin/login", "/admin/verify", "/admin/logout",
		"/admin/leaderboard.json",
		"/admin/leaderboard/flag", "/admin/leaderboard/unflag",
		"/admin/queue.json", "/admin/winners.csv",
		"/admin/settings.json", "/admin/settings/max-concurrent":
		return path
	}
	switch {
	case strings.HasPrefix(path, "/api/queue/status/"):
		return "/api/queue/status/:id"
	case strings.HasPrefix(path, "/api/queue/"):
		return "/api/queue/:id"
	case strings.HasPrefix(path, "/admin/replay/") && strings.HasSuffix(path, "/meta"):
		return "/admin/replay/:id/meta"
	case strings.HasPrefix(path, "/admin/replay/") && strings.HasSuffix(path, "/ticket"):
		return "/admin/replay/:id/ticket"
	case strings.HasPrefix(path, "/admin/replay/"):
		return "/admin/replay/:id"
	case strings.HasPrefix(path, "/admin/leaderboard/"):
		return "/admin/leaderboard/:id"
	case strings.HasPrefix(path, "/admin/queue/"):
		return "/admin/queue/:id"
	case strings.HasPrefix(path, "/admin/slot/free/"):
		return "/admin/slot/free/:id"
	}
	return "other"
}
