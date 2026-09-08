package cfr

import (
	"sort"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/handclass"
)

// Transition is the distribution over hand classes a draw lands in: the
// class at Idx[i] with probability P[i].
type Transition struct {
	Idx []uint16
	P   []float64
}

// Transitions holds every draw the best-response walk can take, as sparse
// class-to-class tables computed once over ranks. Everything is indexed
// by strength position (Ordering), the space the walk's vectors live in.
//
// A table is keyed by the ranks kept, not the hand drawn from: the
// replacements come from the deck less the kept cards, so the discards
// and the opponent's hand are both ignored — the two decks are treated
// as independent, a small consistent error. That is what lets every
// class keeping the same ranks share one table, and one dot product per
// draw node. Suits are ignored past the flush bit: a hand that draws is
// scored as its non-flush rank class, since a flush after a draw is both
// rare and already a loser, and a stand pat keeps its exact class.
type Transitions struct {
	// Tables lists the distinct tables, one per kept rank multiset, with
	// Idx in position space.
	Tables []*Transition
	// NumCand[pos] is the class's candidate count; Cand[pos][i] the table
	// index of candidate i, or Pat for a stand pat; Count[pos][i] how many
	// cards it draws; Keep[pos][i] its mask.
	NumCand []uint8
	Cand    [][MaxCand]int32
	Count   [][MaxCand]uint8
	Keep    [][MaxCand]uint8
	order   *Ordering
	keys    map[uint64]int32
}

// Pat marks a stand pat, which every class maps to itself.
const Pat = -1

// NewTransitions builds the tables for every keep of every live class.
func NewTransitions(a *Abstraction, o *Ordering) *Transitions {
	n := len(o.Order)
	tr := &Transitions{NumCand: make([]uint8, n), Cand: make([][MaxCand]int32, n),
		Count: make([][MaxCand]uint8, n), Keep: make([][MaxCand]uint8, n), order: o, keys: map[uint64]int32{}}
	for pos, id := range o.Order {
		hand := cards.SortedByRank(handclass.Representative(handclass.ID(id)))
		info := &a.Classes[id]
		tr.NumCand[pos] = info.NumCand
		for i := 0; i < int(info.NumCand); i++ {
			kept := keptRanks(hand, info.Keep[i])
			tr.Cand[pos][i] = tr.index(kept)
			tr.Count[pos][i] = uint8(5 - len(kept))
			tr.Keep[pos][i] = info.Keep[i]
		}
		for mask := 0; mask < 1<<5; mask++ {
			tr.index(keptRanks(hand, uint8(mask)))
		}
	}
	return tr
}

// For is the table index for an arbitrary keep mask over the class's
// sorted representative.
func (tr *Transitions) For(class int, keep uint8) int32 {
	hand := cards.SortedByRank(handclass.Representative(handclass.ID(class)))
	key, ok := tr.keys[keptKey(keptRanks(hand, keep))]
	if !ok {
		return Pat
	}
	return key
}

// index finds or builds the table for a kept rank multiset.
func (tr *Transitions) index(kept []cards.Rank) int32 {
	if len(kept) == 5 {
		return Pat
	}
	key := keptKey(kept)
	if i, ok := tr.keys[key]; ok {
		return i
	}
	i := int32(len(tr.Tables))
	t := computeTransition(kept)
	for j, id := range t.Idx {
		t.Idx[j] = uint16(tr.order.Pos[id])
	}
	tr.Tables = append(tr.Tables, t)
	tr.keys[key] = i
	return i
}

func keptRanks(sorted []cards.Card, keep uint8) []cards.Rank {
	var kept []cards.Rank
	for i, card := range sorted {
		if keep&(1<<i) != 0 {
			kept = append(kept, card.Rank)
		}
	}
	return kept
}

func keptKey(kept []cards.Rank) uint64 {
	var key uint64
	for _, rank := range kept {
		key += 1 << (3 * rank.Index())
	}
	return key
}

// computeTransition enumerates the rank multisets the replacement cards
// can form from the deck less the kept cards, weighting each by the ways
// its ranks can be drawn.
func computeTransition(kept []cards.Rank) *Transition {
	var avail [13]int
	for i := range avail {
		avail[i] = 4
	}
	for _, rank := range kept {
		avail[rank.Index()]--
	}
	n := 5 - len(kept)
	total := float64(binomialInt(cards.DeckSize-len(kept), n))
	mass := map[uint16]float64{}
	drawn := make([]cards.Rank, 0, n)
	var rec func(rank int, left int, ways float64)
	rec = func(rank int, left int, ways float64) {
		if left == 0 {
			ranks := append(append([]cards.Rank(nil), kept...), drawn...)
			id := uint16(handclass.Of(withCycledSuits(ranks)))
			mass[id] += ways / total
			return
		}
		if rank == 13 {
			return
		}
		base := len(drawn)
		for k := 0; k <= left && k <= avail[rank]; k++ {
			drawn = drawn[:base]
			for j := 0; j < k; j++ {
				drawn = append(drawn, cards.Two+cards.Rank(rank))
			}
			rec(rank+1, left-k, ways*float64(binomialInt(avail[rank], k)))
		}
		drawn = drawn[:base]
	}
	rec(0, n, 1)
	t := &Transition{Idx: make([]uint16, 0, len(mass)), P: make([]float64, 0, len(mass))}
	for id := range mass {
		t.Idx = append(t.Idx, id)
	}
	sort.Slice(t.Idx, func(i, j int) bool { return t.Idx[i] < t.Idx[j] })
	for _, id := range t.Idx {
		t.P = append(t.P, mass[id])
	}
	return t
}

func ranksOf(hand []cards.Card) []cards.Rank {
	ranks := make([]cards.Rank, len(hand))
	for i, card := range hand {
		ranks[i] = card.Rank
	}
	return ranks
}

// withCycledSuits builds a hand from ranks that is never a flush and never
// repeats a card: sorted so duplicate ranks sit together, then suits by
// position, four suits over five positions.
func withCycledSuits(ranks []cards.Rank) []cards.Card {
	sorted := append([]cards.Rank(nil), ranks...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	suits := [4]cards.Suit{cards.Clubs, cards.Diamonds, cards.Hearts, cards.Spades}
	hand := make([]cards.Card, len(sorted))
	for i, rank := range sorted {
		hand[i] = cards.Card{Rank: rank, Suit: suits[i%4]}
	}
	return hand
}

func binomialInt(n, k int) int {
	if k < 0 || k > n {
		return 0
	}
	result := 1
	for i := 1; i <= k; i++ {
		result = result * (n - k + i) / i
	}
	return result
}

// Ordering sorts the live classes by showdown strength, so a reach vector
// laid out in that order can be scored against every class with one
// prefix sum. Position is the index space of the best-response walk.
type Ordering struct {
	// Order lists live classes worst first: Order[pos] is a class id.
	Order []uint16
	// Pos[class] is the class's position, -1 for a dead class.
	Pos []int32
	// Lo[pos] and Hi[pos] bound the positions tying with pos: those
	// before Lo lose to it, those from Hi on beat it.
	Lo, Hi []int32
	// Weight[pos] is the class's share of all deals.
	Weight []float64
}

// NewOrdering ranks every live class by the engine's evaluator.
func NewOrdering() *Ordering {
	o := &Ordering{Pos: make([]int32, handclass.Num)}
	values := make([]deuce.Value, handclass.Num)
	for id := 0; id < handclass.Num; id++ {
		o.Pos[id] = -1
		if handclass.Weight(handclass.ID(id)) == 0 {
			continue
		}
		values[id] = deuce.Eval(handclass.Representative(handclass.ID(id)))
		o.Order = append(o.Order, uint16(id))
	}
	sort.SliceStable(o.Order, func(i, j int) bool { return values[o.Order[i]] < values[o.Order[j]] })
	n := len(o.Order)
	o.Lo, o.Hi, o.Weight = make([]int32, n), make([]int32, n), make([]float64, n)
	for pos, id := range o.Order {
		o.Pos[id] = int32(pos)
		o.Weight[pos] = float64(handclass.Weight(handclass.ID(id))) / cards.Deals
	}
	for i := 0; i < n; {
		j := i
		for j < n && values[o.Order[j]] == values[o.Order[i]] {
			j++
		}
		for k := i; k < j; k++ {
			o.Lo[k], o.Hi[k] = int32(i), int32(j)
		}
		i = j
	}
	return o
}
