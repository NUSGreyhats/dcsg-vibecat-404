package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// metrics is the shared collector across all synthetic clients. Counters
// are plain atomics; the inter-state-gap sampler keeps a bounded reservoir
// per role so the percentile calc stays O(N log N) on a fixed N regardless
// of test duration.
type metrics struct {
	connAttempts atomic.Int64
	connOK       atomic.Int64
	connFail     atomic.Int64
	authFail     atomic.Int64
	dialFail     atomic.Int64
	httpReject   atomic.Int64 // non-101 HTTP status from /ws or /ws/spectate

	stateRecv     atomic.Int64
	otherRecv     atomic.Int64
	errorRecv     atomic.Int64
	runComplete   atomic.Int64
	disconnects   atomic.Int64
	bytesReceived atomic.Int64

	playerGap    *reservoir // ms between consecutive S2C_STATE on a player WS
	spectatorGap *reservoir // ms between consecutive S2C_STATE on a spectator WS

	rejectReasons sync.Map // map[string]*atomic.Int64 — observed rejection codes
}

func newMetrics() *metrics {
	return &metrics{
		playerGap:    newReservoir(8192),
		spectatorGap: newReservoir(8192),
	}
}

func (m *metrics) recordReject(reason string) {
	v, _ := m.rejectReasons.LoadOrStore(reason, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
}

// snapshot is a frozen view used for periodic and final reports.
type snapshot struct {
	t time.Time

	connAttempts, connOK, connFail int64
	authFail, dialFail, httpReject int64
	stateRecv, otherRecv, errorRecv int64
	runComplete, disconnects        int64
	bytesReceived                   int64

	playerGapP50, playerGapP95, playerGapP99       float64
	spectatorGapP50, spectatorGapP95, spectatorGapP99 float64
	playerGapSamples, spectatorGapSamples          int

	rejectReasons map[string]int64
}

func (m *metrics) snapshot() snapshot {
	s := snapshot{
		t:               time.Now(),
		connAttempts:    m.connAttempts.Load(),
		connOK:          m.connOK.Load(),
		connFail:        m.connFail.Load(),
		authFail:        m.authFail.Load(),
		dialFail:        m.dialFail.Load(),
		httpReject:      m.httpReject.Load(),
		stateRecv:       m.stateRecv.Load(),
		otherRecv:       m.otherRecv.Load(),
		errorRecv:       m.errorRecv.Load(),
		runComplete:     m.runComplete.Load(),
		disconnects:     m.disconnects.Load(),
		bytesReceived:   m.bytesReceived.Load(),
		rejectReasons:   map[string]int64{},
	}
	pSamples := m.playerGap.snapshot()
	sSamples := m.spectatorGap.snapshot()
	s.playerGapSamples = len(pSamples)
	s.spectatorGapSamples = len(sSamples)
	s.playerGapP50, s.playerGapP95, s.playerGapP99 = percentiles(pSamples)
	s.spectatorGapP50, s.spectatorGapP95, s.spectatorGapP99 = percentiles(sSamples)
	m.rejectReasons.Range(func(k, v any) bool {
		s.rejectReasons[k.(string)] = v.(*atomic.Int64).Load()
		return true
	})
	return s
}

func percentiles(samples []float64) (p50, p95, p99 float64) {
	if len(samples) == 0 {
		return 0, 0, 0
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	pick := func(p float64) float64 {
		idx := int(math.Floor(p * float64(len(sorted)-1)))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sorted) {
			idx = len(sorted) - 1
		}
		return sorted[idx]
	}
	return pick(0.50), pick(0.95), pick(0.99)
}

// printDelta prints a single-line periodic report relative to a previous
// snapshot, for tracking rates over the last interval.
func printDelta(prev, cur snapshot) {
	dt := cur.t.Sub(prev.t).Seconds()
	if dt <= 0 {
		dt = 1
	}
	stateRate := float64(cur.stateRecv-prev.stateRecv) / dt
	bytesRate := float64(cur.bytesReceived-prev.bytesReceived) / dt
	fmt.Printf(
		"[%s] conns=%d/%d state=%d (%.0f/s) bytes=%.1fMB/s pGap p95=%.1fms sGap p95=%.1fms err=%d disc=%d\n",
		cur.t.Format("15:04:05"),
		cur.connOK, cur.connAttempts,
		cur.stateRecv, stateRate,
		bytesRate/(1024*1024),
		cur.playerGapP95, cur.spectatorGapP95,
		cur.errorRecv, cur.disconnects,
	)
}

func printFinal(start time.Time, s snapshot) {
	dur := s.t.Sub(start).Seconds()
	if dur <= 0 {
		dur = 1
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n=== load test summary ===\n")
	fmt.Fprintf(&b, "duration:           %.1fs\n", dur)
	fmt.Fprintf(&b, "connections:        attempted=%d ok=%d fail=%d (auth=%d dial=%d http_reject=%d)\n",
		s.connAttempts, s.connOK, s.connFail, s.authFail, s.dialFail, s.httpReject)
	fmt.Fprintf(&b, "state msgs:         %d (%.0f/s)\n", s.stateRecv, float64(s.stateRecv)/dur)
	fmt.Fprintf(&b, "other msgs:         %d\n", s.otherRecv)
	fmt.Fprintf(&b, "error msgs (S2C):   %d\n", s.errorRecv)
	fmt.Fprintf(&b, "run_complete msgs:  %d\n", s.runComplete)
	fmt.Fprintf(&b, "disconnects:        %d\n", s.disconnects)
	fmt.Fprintf(&b, "bytes received:     %.1f MB (%.2f MB/s avg)\n",
		float64(s.bytesReceived)/(1024*1024),
		float64(s.bytesReceived)/(1024*1024)/dur)
	if s.playerGapSamples > 0 {
		fmt.Fprintf(&b, "player tick gap:    p50=%.1fms p95=%.1fms p99=%.1fms (n=%d, target=50ms)\n",
			s.playerGapP50, s.playerGapP95, s.playerGapP99, s.playerGapSamples)
	}
	if s.spectatorGapSamples > 0 {
		fmt.Fprintf(&b, "spectator tick gap: p50=%.1fms p95=%.1fms p99=%.1fms (n=%d, target=50ms)\n",
			s.spectatorGapP50, s.spectatorGapP95, s.spectatorGapP99, s.spectatorGapSamples)
	}
	if len(s.rejectReasons) > 0 {
		fmt.Fprintf(&b, "rejection codes:\n")
		keys := make([]string, 0, len(s.rejectReasons))
		for k := range s.rejectReasons {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "  %-32s %d\n", k, s.rejectReasons[k])
		}
	}
	fmt.Print(b.String())
}
