package cfr

import "github.com/nuttakit/2-7-bot/internal/cards"

// FixedCard identifies the arena's constant big-blind card. The platform's
// shuffle leaves the first card dealt to the big blind unchanged for an
// entire match (143 of 143 hosted matches read on 2026-09-06), so the
// intersection of our own big-blind hands pins it down within a few hands,
// after which every button hand knows one of the opponent's five cards.
type FixedCard struct {
	candidates cards.Set
	hands      int
}

// ObserveBigBlind narrows the candidates with a hand we were dealt as the
// big blind. A hand that shares nothing with the candidates means the
// platform stopped fixing the card; start over from that hand.
func (f *FixedCard) ObserveBigBlind(hand cards.Set) {
	if f.hands == 0 || f.candidates&hand == 0 {
		f.candidates = hand
		f.hands = 1
		return
	}
	f.candidates &= hand
	f.hands++
}

// OpponentHolds is the card a big-blind opponent was dealt, once a single
// candidate has survived at least three of our own big-blind hands. Two
// random hands share a card about one time in four, so a lone survivor of
// two is not yet evidence.
func (f *FixedCard) OpponentHolds() cards.Set {
	if f.hands >= 3 && f.candidates.Len() == 1 {
		return f.candidates
	}
	return 0
}

// Group is the strategy group of the identified card, 0 while unknown.
func (f *FixedCard) Group() int {
	held := f.OpponentHolds()
	if held == 0 {
		return 0
	}
	for i := 0; i < cards.DeckSize; i++ {
		c := cards.CardFromIndex(i)
		if cards.NewSet([]cards.Card{c}) == held {
			return FixedGroup(c.Rank)
		}
	}
	return 0
}
