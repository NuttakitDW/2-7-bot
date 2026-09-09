package cfr

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/handclass"
)

// RiverPolicy is a sparse response at the final betting round. Earlier
// decisions and unvisited river states remain the base policy's decisions.
type RiverPolicy struct {
	Version     int        `json:"version"`
	Abstraction string     `json:"abstraction"`
	Iterations  int64      `json:"iterations"`
	Rows        []RiverRow `json:"rows"`
}

type RiverRow struct {
	Key    uint64   `json:"key"`
	P      [3]uint8 `json:"p"` // Fold, Pass, Aggr, independent of legal-action indices.
	Visits uint64   `json:"visits"`
}

// ResponseBundle carries the frozen fitted policy and sparse river overlay
// together, so a runtime artifact needs no sidecar files.
type ResponseBundle struct {
	Version int             `json:"response_version"`
	Base    json.RawMessage `json:"base"`
	River   []byte          `json:"river"`
}

func EncodeRiverResponse(base []byte, policy *RiverPolicy, w io.Writer) error {
	var data bytes.Buffer
	if err := policy.Encode(&data); err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(ResponseBundle{Version: 1, Base: base, River: data.Bytes()})
}

func DecodeRiverResponse(raw []byte, tree *Tree) (*RiverResponse, error) {
	var bundle ResponseBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return nil, err
	}
	if bundle.Version != 1 {
		return nil, fmt.Errorf("unsupported response version %d", bundle.Version)
	}
	base, err := DecodeEmpirical(bundle.Base)
	if err != nil {
		return nil, fmt.Errorf("response base: %w", err)
	}
	policy, err := DecodeRiverPolicy(bundle.River)
	if err != nil {
		return nil, fmt.Errorf("response river: %w", err)
	}
	return NewRiverResponse(base.Mode(), policy, tree), nil
}

func validRiverAbstraction(name string) bool {
	return name == "rank" || name == "class" || name == "legacy"
}

func (p *RiverPolicy) Encode(w io.Writer) error {
	z := gzip.NewWriter(w)
	if err := json.NewEncoder(z).Encode(p); err != nil {
		_ = z.Close()
		return err
	}
	return z.Close()
}

func DecodeRiverPolicy(raw []byte) (*RiverPolicy, error) {
	z, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer z.Close()
	const maximum = 300 << 20
	data, err := io.ReadAll(io.LimitReader(z, maximum+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maximum {
		return nil, fmt.Errorf("river response exceeds size limit")
	}
	var p RiverPolicy
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	if p.Version != 1 || !validRiverAbstraction(p.Abstraction) || p.Iterations < 0 {
		return nil, fmt.Errorf("invalid river response header")
	}
	seen := make(map[uint64]bool, len(p.Rows))
	for _, row := range p.Rows {
		if seen[row.Key] || int(row.P[0])+int(row.P[1])+int(row.P[2]) != 255 || row.Visits == 0 {
			return nil, fmt.Errorf("invalid or duplicate river row %d", row.Key)
		}
		seen[row.Key] = true
	}
	return &p, nil
}

// RiverResponse exposes only the same View as every other policy. Training
// may simulate hidden cards, but a runtime key contains no opponent cards.
type RiverResponse struct {
	Base      Model
	Policy    *RiverPolicy
	Greedy    bool
	MinVisits uint64
	contexts  []uint32
	rows      map[uint64]RiverRow
}

func NewRiverResponse(base Model, policy *RiverPolicy, tree *Tree) *RiverResponse {
	r := &RiverResponse{Base: base, Policy: policy, contexts: newRiverContexts(tree), rows: make(map[uint64]RiverRow, len(policy.Rows))}
	for _, row := range policy.Rows {
		r.rows[row.Key] = row
	}
	return r
}

func (r *RiverResponse) Bet(v *View) (int, bool) {
	if v.Street == Draw3 && v.Node >= 0 && int(v.Node) < len(r.contexts) {
		row, ok := r.rows[riverKey(v, r.contexts, r.Policy.Abstraction)]
		if ok && row.Visits >= r.MinVisits {
			var p [6]float64
			for i, x := range row.P {
				p[i] = float64(x)
			}
			if !v.Facing {
				p[Fold] = 0
			}
			if !v.CanRaise {
				p[Aggr] = 0
			}
			if r.Greedy {
				return mostLikelyPolicy(p, 3)
			}
			if action, ok := samplePolicy(p, 3, v.Rand); ok {
				return action, true
			}
		}
	}
	return r.Base.Bet(v)
}

func (r *RiverResponse) Draw(v *View) (uint8, bool) { return r.Base.Draw(v) }

var riverRankBuckets = sync.OnceValue(func() *[handclass.Num]uint16 {
	out := new([handclass.Num]uint16)
	groups := map[uint32]uint16{}
	for id := range out {
		if handclass.Weight(handclass.ID(id)) == 0 {
			continue
		}
		hand := handclass.Representative(handclass.ID(id))
		value := deuce.Eval(hand)
		key := richFinalKey(hand)
		// Keep every jack-or-better low exact. Weaker no-pair hands retain
		// their top rank, pairs their paired rank; two-pair-or-worse hands
		// share the legacy junk bucket. Suits matter only for flushes.
		if value.Class() == deuce.HighCard && cards.DistinctRanks(hand)[4] <= cards.Jack {
			key = uint32(value)
		}
		bucket, ok := groups[key]
		if !ok {
			bucket = uint16(len(groups))
			groups[key] = bucket
		}
		out[id] = bucket
	}
	return out
})

func riverHandBucket(hand [5]cards.Card, abstraction string) uint16 {
	switch abstraction {
	case "class":
		return uint16(handclass.Of(hand[:]))
	case "legacy":
		return uint16(finalBucket(hand[:]))
	default:
		return riverRankBuckets()[handclass.Of(hand[:])]
	}
}

// River contexts retain the exact within-river betting sequence, starting
// pot, each seat's preceding-street aggression, and prior wager counts.
// Earlier action order is deliberately aggregated to improve sample density.
func newRiverContexts(tree *Tree) []uint32 {
	out := make([]uint32, len(tree.Nodes))
	var walk func(int32, uint32, [Streets][2]uint32, uint32)
	walk = func(id int32, path uint32, raises [Streets][2]uint32, prefix uint32) {
		n := &tree.Nodes[id]
		if n.Kind == KindFold || n.Kind == KindShowdown {
			return
		}
		if n.Kind == KindDraw {
			walk(n.Next[0], path, raises, prefix)
			return
		}
		if n.Street == Draw3 {
			if n.Wagers == 0 && n.Actor == BB {
				path = 1
				prefix = uint32((n.Commit[0] + n.Commit[1]) / 100)
				prefix = prefix<<3 | raises[Draw2][0]
				prefix = prefix<<3 | raises[Draw2][1]
				prefix = prefix<<3 | (raises[Draw1][0] + raises[Draw1][1])
				prefix = prefix<<3 | (raises[Predraw][0] + raises[Predraw][1])
			}
			out[id] = prefix<<9 | path
		}
		for i, a := range n.Acts {
			nextRaises := raises
			if a == Aggr {
				nextRaises[n.Street][n.Actor]++
			}
			nextPath := path
			if n.Street == Draw3 {
				nextPath = path * 2
				if a == Aggr {
					nextPath++
				}
			}
			walk(n.Next[i], nextPath, nextRaises, prefix)
		}
	}
	walk(tree.Root, 0, [Streets][2]uint32{}, 0)
	return out
}

func riverKey(v *View, contexts []uint32, abstraction string) uint64 {
	key := uint64(contexts[v.Node])<<13 | uint64(riverHandBucket(v.Hand, abstraction))
	key = key<<1 | uint64(v.Seat)
	for p := 0; p < 2; p++ {
		for s := Draw2; s <= Draw3; s++ {
			key = key<<2 | uint64(clip(int(v.Drawn[p][s])))
		}
	}
	return key<<2 | uint64(AggrState(v.Seat, v.LastAggr))
}
