package cfr

import (
	"sync"
	"sync/atomic"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/handclass"
)

// BlueprintStrategy reads a Blueprint as a Strategy, with the same purify
// floor and greedy switch the Player applies, so what is measured is what
// is played. Untrained sets go to Fallback, the model the bot would defer
// to; with no Fallback they pass and keep the structural draw.
type BlueprintStrategy struct {
	Player   *Player
	Fallback Model
	Order    *Ordering
	// Group is the big-blind-card group the button's sets are read in.
	Group int

	// Queries and Untrained count set reads, and the reads that had no
	// trained answer, across both kinds of decision.
	Queries, Untrained atomic.Int64

	bucket [Streets][]uint16
	// cache holds the fallback's answers per public state: a table of
	// one slot per position, filled on first use without locking.
	cache sync.Map
}

// fallbackTable caches one public state's fallback answers by position:
// 0 for not yet asked, otherwise the action or candidate index plus one.
type fallbackTable struct {
	answers []atomic.Uint32
}

// NewBlueprintStrategy wraps a player over an ordering; fallback may be nil.
func NewBlueprintStrategy(pl *Player, fallback Model, o *Ordering) *BlueprintStrategy {
	s := &BlueprintStrategy{Player: pl, Fallback: fallback, Order: o}
	for street := range s.bucket {
		s.bucket[street] = make([]uint16, len(o.Order))
		for pos, id := range o.Order {
			s.bucket[street][pos] = uint16(pl.Abs.Bucket(street, int(id)))
		}
	}
	return s
}

// bucketMix is one bucket's answer at a node: untrained, or a mixture.
type bucketMix struct {
	seen, trained bool
	probs         [3]float64
}

// Bet implements Strategy. The blueprint answers per bucket, so each
// bucket's mixture is spread once and the classes only multiply.
func (s *BlueprintStrategy) Bet(node *Node, drawn *[2]DrawCounts, lastAggr int, reach []float64, out [][]float64) [3]float64 {
	pl := s.Player
	p, street := int(node.Actor), int(node.Street)
	n := len(node.Acts)
	ctx := BetContext(p, street, drawn)
	mixes := make([]bucketMix, pl.Layout.Buckets(street))
	buckets := s.bucket[street]
	var masses [3]float64
	var table *fallbackTable
	queries, untrained := 0, 0
	for pos, r := range reach {
		if r == 0 {
			continue
		}
		queries++
		mix := &mixes[buckets[pos]]
		if !mix.seen {
			mix.seen = true
			slot := pl.Layout.BetSlotFixed(node, ctx, int(buckets[pos]), s.Group)
			mix.trained = s.spread(pl.BP.Bet[slot:slot+int64(n)], mix.probs[:n])
		}
		if mix.trained {
			for a := 0; a < n; a++ {
				out[a][pos] = r * mix.probs[a]
				masses[a] += out[a][pos]
			}
			continue
		}
		untrained++
		if table == nil {
			table = s.table(fallbackKey(0, p, street, drawn, lastAggr, int(node.Wagers), node.Facing))
		}
		a := s.fallbackBet(table, pos, node, drawn, lastAggr)
		out[a][pos] = r
		masses[a] += r
	}
	s.Queries.Add(int64(queries))
	s.Untrained.Add(int64(untrained))
	return masses
}

// Draw implements Strategy.
func (s *BlueprintStrategy) Draw(street, p, lastAggr int, drawn *[2]DrawCounts, reach []float64, emit func(pos, cand int, mass float64)) {
	pl := s.Player
	aggr, ctx := AggrState(p, lastAggr), DrawContext(p, street, drawn)
	var probs [MaxCand]float64
	var table *fallbackTable
	queries, untrained := 0, 0
	for pos, r := range reach {
		if r == 0 {
			continue
		}
		queries++
		class := int(s.Order.Order[pos])
		info := &pl.Abs.Classes[class]
		n := int(info.NumCand)
		slot := pl.Layout.DrawSlotFixed(street, p, aggr, ctx, int(info.DrawClass), s.Group)
		if s.spread(pl.BP.Draw[slot:slot+int64(n)], probs[:n]) {
			for i := 0; i < n; i++ {
				if probs[i] > 0 {
					emit(pos, i, r*probs[i])
				}
			}
			continue
		}
		untrained++
		if table == nil {
			table = s.table(fallbackKey(1, p, street, drawn, lastAggr, 0, false))
		}
		emit(pos, s.fallbackDraw(table, pos, street, p, lastAggr, drawn), r)
	}
	s.Queries.Add(int64(queries))
	s.Untrained.Add(int64(untrained))
}

// spread turns a quantised set into probabilities the way Player.choose
// samples it; false means the set is untrained.
func (s *BlueprintStrategy) spread(bytes []uint8, probs []float64) bool {
	pl := s.Player
	if pl.Greedy {
		best := 0
		for i, b := range bytes {
			if b > bytes[best] {
				best = i
			}
		}
		if bytes[best] == 0 {
			return false
		}
		clear(probs)
		probs[best] = 1
		return true
	}
	floor := uint8(pl.Purify * 255)
	sum := 0
	for _, b := range bytes {
		if b >= floor {
			sum += int(b)
		}
	}
	if sum == 0 {
		return false
	}
	for i, b := range bytes {
		if b >= floor {
			probs[i] = float64(b) / float64(sum)
		} else {
			probs[i] = 0
		}
	}
	return true
}

func (s *BlueprintStrategy) fallbackBet(table *fallbackTable, pos int, node *Node, drawn *[2]DrawCounts, lastAggr int) int {
	if s.Fallback == nil {
		return actionIndex(node, Pass, true)
	}
	if cached := table.answers[pos].Load(); cached != 0 {
		return int(cached - 1)
	}
	v := s.view(int(node.Actor), int(node.Street), drawn, lastAggr, int(s.Order.Order[pos]))
	v.Pot = node.Commit[0] + node.Commit[1]
	v.ToCall = max(0, node.Commit[1-v.Seat]-node.Commit[v.Seat])
	v.Facing = node.Facing
	v.Wagers = int(node.Wagers)
	v.CanRaise = node.Acts[len(node.Acts)-1] == Aggr
	action, ok := s.Fallback.Bet(&v)
	i := actionIndex(node, action, ok)
	table.answers[pos].Store(uint32(i + 1))
	return i
}

// fallbackDraw maps the fallback's keep onto the class's candidate list,
// taking the structural keep when the fallback's is not among them.
func (s *BlueprintStrategy) fallbackDraw(table *fallbackTable, pos, street, p, lastAggr int, drawn *[2]DrawCounts) int {
	class := int(s.Order.Order[pos])
	info := &s.Player.Abs.Classes[class]
	structural := min(1, int(info.NumCand)-1)
	if s.Fallback == nil {
		return structural
	}
	if cached := table.answers[pos].Load(); cached != 0 {
		return int(cached - 1)
	}
	v := s.view(p, street, drawn, lastAggr, class)
	keep, ok := s.Fallback.Draw(&v)
	i := structural
	if ok {
		for j := 0; j < int(info.NumCand); j++ {
			if info.Keep[j] == keep {
				i = j
				break
			}
		}
	}
	table.answers[pos].Store(uint32(i + 1))
	return i
}

func (s *BlueprintStrategy) view(p, street int, drawn *[2]DrawCounts, lastAggr, class int) View {
	v := View{Seat: p, Street: street, Drawn: *drawn, LastAggr: lastAggr, Rand: 0.5}
	if p == Btn {
		v.FixedGroup = s.Group
	}
	copy(v.Hand[:], cards.SortedByRank(handclass.Representative(handclass.ID(class))))
	return v
}

// fallbackKey packs the public state a fallback decision reads: the
// heuristics see the street, the wager count, whether they face a bet,
// and the opponent's latest draw count with how many streets ago it was
// made (table.Hand.OpponentDraw) — not the betting order, and not their
// own draws. Keying on just that keeps the cache to a few thousand
// tables where the full draw record would need millions.
func fallbackKey(kind, p, street int, drawn *[2]DrawCounts, lastAggr, wagers int, facing bool) uint64 {
	key := uint64(kind)<<1 | uint64(p)
	key = key<<2 | uint64(street)
	count, ago := 0, 0
	for s := street; s >= Draw1; s-- {
		if drawn[1-p][s] >= 0 {
			count, ago = int(drawn[1-p][s])+1, street-s
			break
		}
	}
	key = key<<3 | uint64(count)
	key = key<<2 | uint64(ago)
	key = key<<2 | uint64(lastAggr+1)
	key = key<<3 | uint64(wagers)
	if facing {
		key |= 1 << 63
	}
	return key
}

// table is the answer table for a public state, made on first use.
func (s *BlueprintStrategy) table(key uint64) *fallbackTable {
	if t, ok := s.cache.Load(key); ok {
		return t.(*fallbackTable)
	}
	t, _ := s.cache.LoadOrStore(key, &fallbackTable{answers: make([]atomic.Uint32, len(s.Order.Order))})
	return t.(*fallbackTable)
}

// FoldStrategy folds whenever it may, checks otherwise, and stands pat: the
// strategy whose exploitability is exactly the blinds.
type FoldStrategy struct{}

// Bet implements Strategy.
func (FoldStrategy) Bet(node *Node, _ *[2]DrawCounts, _ int, reach []float64, out [][]float64) [3]float64 {
	a := actionIndex(node, Fold, true)
	copy(out[a], reach)
	var masses [3]float64
	masses[a] = total(reach)
	return masses
}

// Draw implements Strategy.
func (FoldStrategy) Draw(_, _, _ int, _ *[2]DrawCounts, reach []float64, emit func(pos, cand int, mass float64)) {
	for pos, r := range reach {
		if r != 0 {
			emit(pos, 0, r)
		}
	}
}

// UniformStrategy mixes every action evenly, and every candidate keep on
// the streets up to DrawUntil, standing pat after.
type UniformStrategy struct {
	Trans     *Transitions
	DrawUntil int
}

// Bet implements Strategy.
func (UniformStrategy) Bet(_ *Node, _ *[2]DrawCounts, _ int, reach []float64, out [][]float64) [3]float64 {
	share := 1 / float64(len(out))
	var masses [3]float64
	for a := range out {
		for pos, r := range reach {
			out[a][pos] = r * share
			masses[a] += out[a][pos]
		}
	}
	return masses
}

// Draw implements Strategy.
func (u UniformStrategy) Draw(street, _, _ int, _ *[2]DrawCounts, reach []float64, emit func(pos, cand int, mass float64)) {
	for pos, r := range reach {
		if r == 0 {
			continue
		}
		if street > u.DrawUntil {
			emit(pos, 0, r)
			continue
		}
		n := int(u.Trans.NumCand[pos])
		for i := 0; i < n; i++ {
			emit(pos, i, r/float64(n))
		}
	}
}
