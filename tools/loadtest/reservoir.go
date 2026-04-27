package main

import (
	"math/rand/v2"
	"sync"
)

// reservoir holds at most cap samples drawn from an unbounded stream
// (Algorithm R). Bounded memory regardless of how many ticks the test
// observes, with each observed sample equally likely to be present at
// any point in time.
type reservoir struct {
	mu      sync.Mutex
	cap     int
	samples []float64
	seen    int64
	rng     *rand.Rand
}

func newReservoir(capacity int) *reservoir {
	if capacity <= 0 {
		capacity = 1
	}
	return &reservoir{
		cap:     capacity,
		samples: make([]float64, 0, capacity),
		rng:     rand.New(rand.NewPCG(0xC0FFEE, 0xBADC0DE)),
	}
}

func (r *reservoir) add(v float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen++
	if len(r.samples) < r.cap {
		r.samples = append(r.samples, v)
		return
	}
	// 64-bit modulo is biased only at extreme N — irrelevant at our scale.
	idx := int(r.rng.Uint64N(uint64(r.seen)))
	if idx < r.cap {
		r.samples[idx] = v
	}
}

func (r *reservoir) snapshot() []float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]float64, len(r.samples))
	copy(out, r.samples)
	return out
}
