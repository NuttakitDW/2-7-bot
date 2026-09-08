package cfr

import (
	"sync/atomic"

	"github.com/nuttakit/2-7-bot/internal/handclass"
)

// policyMemo is a direct-mapped prediction cache shared by every walker.
// Every feature the model reads is a function of the public node, the draw
// record, the last aggressor and the hand class, so a prediction keyed on
// those is exact; only the sampling draw (View.Rand) varies between calls,
// and it is applied after lookup. A collision simply evicts: this is a
// cache, and a miss costs one forest walk.
//
// Entries are written value first and key last, with the key cleared
// before the value changes; a reader that sees the same key before and
// after copying the value has a consistent entry. Two walkers computing the
// same miss store identical values, so a race between them is harmless.
type policyMemo struct {
	entries []memoEntry
	bits    uint
}

type memoEntry struct {
	key  atomic.Uint64
	vals [6]float32
}

const memoBits = 25

func newPolicyMemo(bits uint) *policyMemo {
	return &policyMemo{entries: make([]memoEntry, 1<<bits), bits: bits}
}

// WithMemo returns a model sharing this one's forests and a prediction
// cache. Every walker may share the returned model.
func (m *Empirical) WithMemo() Model {
	c := &Empirical{Version: m.Version, Betting: m.Betting, Drawing: m.Drawing,
		BettingForest: m.BettingForest, DrawingForest: m.DrawingForest, compiled: m.compile()}
	c.memo = newPolicyMemo(memoBits)
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
	e := &pm.entries[(key*0x9E3779B97F4A7C15)>>(64-pm.bits)]
	if e.key.Load() == key {
		vals := e.vals
		if e.key.Load() == key {
			var out [6]float64
			for k, x := range vals {
				out[k] = float64(x)
			}
			return out
		}
	}
	p := compute(v)
	e.key.Store(0)
	for k, x := range p {
		e.vals[k] = float32(x)
	}
	e.key.Store(key)
	return p
}
