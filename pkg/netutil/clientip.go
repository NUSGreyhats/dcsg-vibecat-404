// Package netutil holds small networking helpers shared by the lobby
// and game-server binaries. The first inhabitant is ClientIP — the
// canonical "what IP do we attribute this request to for rate-limiting
// purposes" function.
package netutil

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP returns the request's client IP for use as a rate-limit /
// audit key. Behavior:
//
//   - If the immediate peer (r.RemoteAddr) is loopback or RFC1918
//     private, we trust X-Forwarded-For (XFF) and return its first
//     entry. This is the docker-compose case: traffic enters via Caddy
//     on the bridge network, and the Caddy container sets XFF to the
//     real client IP.
//   - Otherwise (peer is a public IP) XFF is attacker-controlled and
//     ignored. Returning the peer host stops a single client from
//     trivially evading per-IP rate limits by rotating XFF.
//   - On any parse failure, returns r.RemoteAddr verbatim. Worse than
//     useful, but never crashes.
//
// Centralizing this logic means identity, queue, admin, and spectator
// all rate-limit on the same notion of "client", and trusted-proxy
// changes only need to land in one place.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !IsPrivatePeer(host) {
		return host
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// First entry is the originating client per RFC 7239. Any
		// subsequent entries are the proxies between us and them.
		if comma := strings.IndexByte(xff, ','); comma >= 0 {
			return strings.TrimSpace(xff[:comma])
		}
		return strings.TrimSpace(xff)
	}
	return host
}

// IsPrivatePeer reports whether host is a loopback or RFC1918 / ULA
// private address — the set of peers we trust to set X-Forwarded-For
// and to call /internal/* endpoints. Both the lobby service (docker
// bridge network) and any reverse proxy (Caddy) fall in this set;
// arbitrary internet peers do not.
func IsPrivatePeer(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}
