package cfr

import (
	"fmt"
	"sync"

	"github.com/nuttakit/2-7-bot/internal/handclass"
)

// policyMemo caches predictions keyed on the public node, draw record,
// aggressor and hand class. Random action sampling happens after lookup.
// Stored float32 probabilities introduce small rounding on cache hits.
// Sharded locks protect each entry's key and values as one coherent update;
// forest computation stays outside the locks.
type policyMemo struct {
	entries []memoEntry
	bits    uint
	locks   [256]sync.Mutex
}

type memoEntry struct {
	key  uint64
	vals [6]float32
}

const memoBits = 25

func newPolicyMemo(bits uint) *policyMemo {
	return &policyMemo{entries: make([]memoEntry, 1<<bits), bits: bits}
}

// WithMemo returns a model sharing this one's forests and a prediction
// cache. Every walker may share the returned model.
func (m *Empirical) WithMemo() Model {
	return m.withMemoBits(memoBits)
}

// WithMemoBits returns a cached model with 2^bits entries (32 bytes each).
// A small cache supports runtime belief filtering without the trainer's
// much larger default allocation. The returned cache is concurrency safe.
func (m *Empirical) WithMemoBits(bits uint) (*Empirical, error) {
	if bits < 1 || bits > 25 {
		return nil, fmt.Errorf("memo bits must be between 1 and 25")
	}
	return m.withMemoBits(bits), nil
}

func (m *Empirical) withMemoBits(bits uint) *Empirical {
	c := &Empirical{Version: m.Version, Betting: m.Betting, Drawing: m.Drawing,
		BettingForest: m.BettingForest, DrawingForest: m.DrawingForest,
		BettingBoost: m.BettingBoost, DrawingBoost: m.DrawingBoost,
		ResponseAlpha: m.ResponseAlpha, compiled: m.compile()}
	c.memo = newPolicyMemo(bits)
	return c
}

func memoKey(v *View, draw bool) (uint64, bool) {
	if v.Node < 0 || v.Node >= 1<<20 {
		return 0, false
	}
	key := uint64(v.Node)
	key = key<<13 | uint64(handclass.Of(v.Hand[:]))
	for p := range v.Drawn {
		for s := 1; s < Streets; s++ {
			key = key<<3 | uint64(v.Drawn[p][s]+1)
		}
	}
	key = key<<2 | uint64(v.LastAggr+1)
	key = key<<1 | uint64(v.Seat&1)
	key = key<<3 | uint64(v.Wagers&7)
	key = key<<2 | uint64(v.Street&3)
	if v.Facing {
		key |= 1 << 62
	}
	if v.CanRaise {
		key |= 1 << 61
	}
	if draw {
		key |= 1 << 63
	}
	// Zero marks an empty slot, and no live key is zero: node 0 is the
	// predraw root, but its class and stakes bits are never all clear.
	return key, key != 0
}

func (pm *policyMemo) lookup(v *View, draw bool, compute func(*View) [6]float64) [6]float64 {
	key, ok := memoKey(v, draw)
	if !ok {
		return compute(v)
	}
	index := (key * 0x9E3779B97F4A7C15) >> (64 - pm.bits)
	e := &pm.entries[index]
	lock := &pm.locks[index%uint64(len(pm.locks))]
	lock.Lock()
	if e.key == key {
		var out [6]float64
		for k, x := range e.vals {
			out[k] = float64(x)
		}
		lock.Unlock()
		return out
	}
	lock.Unlock()
	p := compute(v)
	lock.Lock()
	for k, x := range p {
		e.vals[k] = float32(x)
	}
	e.key = key
	lock.Unlock()
	return p
}
