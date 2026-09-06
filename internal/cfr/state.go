package cfr

import (
	"math/rand/v2"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
)

// State is one hand in flight: both hands, the undealt deck, and the
// public draw record. It is small enough to copy at every branch.
type State struct {
	Hands    [2][5]cards.Card
	Deck     [cards.DeckSize]cards.Card
	Ptr      int
	Drawn    [2]DrawCounts
	LastAggr int
}

// Deal shuffles and deals a fresh hand: five cards each, button first.
// Replacement cards come off the top in draw order, which is exactly what
// the engine does, and discards are never recycled — two seats cannot run
// a 52-card deck out (docs/game/rules.md).
func (s *State) Deal(rng *rand.Rand) {
	for i := range s.Deck {
		s.Deck[i] = cards.CardFromIndex(i)
	}
	for i := len(s.Deck) - 1; i > 0; i-- {
		j := rng.IntN(i + 1)
		s.Deck[i], s.Deck[j] = s.Deck[j], s.Deck[i]
	}
	s.Reset()
}

// Reset starts the hand over on the same shuffled deck, which is how a
// duplicate match replays a deal with the seats swapped.
func (s *State) Reset() {
	copy(s.Hands[Btn][:], s.Deck[0:5])
	copy(s.Hands[BB][:], s.Deck[5:10])
	s.Ptr = 10
	sortHand(&s.Hands[Btn])
	sortHand(&s.Hands[BB])
	for p := range s.Drawn {
		for street := range s.Drawn[p] {
			s.Drawn[p][street] = -1
		}
	}
	s.LastAggr = -1
}

// Apply draws for player p on a street: every card outside the keep mask
// is replaced from the deck, and the count is published.
func (s *State) Apply(p, street int, keep uint8) {
	count := 0
	for i := 0; i < 5; i++ {
		if keep&(1<<i) == 0 {
			s.Hands[p][i] = s.Deck[s.Ptr]
			s.Ptr++
			count++
		}
	}
	sortHand(&s.Hands[p])
	s.Drawn[p][street] = int8(count)
}

// Winner scores the showdown: the seat with the better hand, or -1 for a
// split.
func (s *State) Winner(eval deuce.Table) int {
	btn := eval.Value(cards.NewSet(s.Hands[Btn][:]))
	bb := eval.Value(cards.NewSet(s.Hands[BB][:]))
	switch {
	case btn > bb:
		return Btn
	case bb > btn:
		return BB
	default:
		return -1
	}
}

// View assembles what player p sees at a node.
func (s *State) View(t *Tree, id int32, p int, rng *rand.Rand) View {
	node := &t.Nodes[id]
	v := View{Hand: s.Hands[p], Seat: p, Node: id, Street: int(node.Street),
		Drawn: s.Drawn, LastAggr: s.LastAggr, Rand: rng.Float64()}
	if node.Kind == KindBet {
		v.Facing = node.Facing
		v.Wagers = int(node.Wagers)
		v.CanRaise = node.Acts[len(node.Acts)-1] == Aggr
	}
	return v
}

// sortHand orders five cards deuce first, suits ascending within a rank —
// the order every keep mask is read against.
func sortHand(hand *[5]cards.Card) {
	for i := 1; i < 5; i++ {
		for j := i; j > 0 && less(hand[j], hand[j-1]); j-- {
			hand[j], hand[j-1] = hand[j-1], hand[j]
		}
	}
}

func less(a, b cards.Card) bool {
	if a.Rank != b.Rank {
		return a.Rank < b.Rank
	}
	return a.Suit < b.Suit
}
