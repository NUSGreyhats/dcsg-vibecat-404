package metrics

import (
	"net/http"
	"testing"
)

func TestNormaliseMethod(t *testing.T) {
	cases := map[string]string{
		http.MethodGet:    "GET",
		http.MethodPost:   "POST",
		http.MethodDelete: "DELETE",
		"":                "other",
		"FOO":             "other",
		"propfind":        "other",
	}
	for in, want := range cases {
		if got := normaliseMethod(in); got != want {
			t.Errorf("normaliseMethod(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormaliseRoute(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/api/health", "/api/health"},
		{"/api/queue/join", "/api/queue/join"},
		{"/api/queue/status/0123456789abcdef0123456789abcdef", "/api/queue/status/:id"},
		{"/api/queue/0123456789abcdef0123456789abcdef", "/api/queue/:id"},
		{"/admin/", "/admin/"},
		{"/admin/leaderboard.json", "/admin/leaderboard.json"},
		{"/admin/leaderboard/abc-replay-id", "/admin/leaderboard/:id"},
		{"/admin/replay/abc/meta", "/admin/replay/:id/meta"},
		{"/admin/replay/abc/ticket", "/admin/replay/:id/ticket"},
		{"/admin/replay/abc", "/admin/replay/:id"},
		{"/admin/queue/abc", "/admin/queue/:id"},
		{"/admin/slot/free/abc", "/admin/slot/free/:id"},
		{"/random/scanner/probe", "other"},
	}
	for _, tc := range cases {
		if got := NormaliseRoute(tc.in); got != tc.want {
			t.Errorf("NormaliseRoute(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
