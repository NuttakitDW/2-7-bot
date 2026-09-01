// Package equity estimates predraw showdown equity by Monte Carlo rollout.
//
// A rollout deals the villain from the cards the hero does not hold, then
// plays both seats through the three draws with h1's own draw rule
// (policy.DrawDiscards) on the real information timeline — the big blind
// draws first each street and sees only the button's previous count, the
// button draws second and sees the current one — and scores the showdown
// with the engine-exact evaluator.
//
// What comes out is showdown equity under a fixed draw policy, with no
// betting model. That is deliberately less than an EV: it is the *ranking*
// input the chart generator cuts into ranges, and hand orderings are robust
// to the modeling shortcuts that absolute equities are not.
package equity

import (
	"math/rand"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/policy"
	"github.com/nuttakit/2-7-bot/internal/table"
)

// Accept restricts the villain's dealt hands to a range of classes. A nil
// Accept means any five cards — the uniform villain the ranking pass uses.
// Rejection sampling against the deal distribution keeps class weights
// correct without any explicit weighting.
type Accept func(handclass.ID) bool

// maxRejects bounds the rejection loop. Any range wide enough to be a
// strategy accepts within a few tries; hitting the cap means the caller
// passed a near-empty range, which is a bug worth crashing on.
const maxRejects = 100000

// Showdown estimates the hero's showdown equity — wins plus half of ties —
// over n sampled deals and playouts. Identical inputs give identical
// results: the generator's output must be reproducible from its recorded
// seed.
func Showdown(hero []cards.Card, opp Accept, n int, seed int64) float64 {
	if n <= 0 {
		panic("equity: Showdown needs a positive sample count")
	}
	rng := rand.New(rand.NewSource(seed))
	dealer := newDealer(hero, rng)

	heroHand := make([]cards.Card, len(hero))
	villainHand := make([]cards.Card, deuce.HandSize)
	won := 0.0
	for i := 0; i < n; i++ {
		copy(heroHand, hero)
		dealVillain(dealer, villainHand, opp)
		won += playout(dealer, heroHand, villainHand, i%2 == 0)
	}
	return won / float64(n)
}

// dealVillain deals five cards into villain, redealing until the range
// accepts them.
func dealVillain(d *dealer, villain []cards.Card, opp Accept) {
	for tries := 0; tries < maxRejects; tries++ {
		d.reset()
		for i := range villain {
			villain[i] = d.deal()
		}
		if opp == nil || opp(handclass.Of(villain)) {
			return
		}
	}
	panic("equity: villain range rejected every sampled hand")
}

// playout runs the three draw streets and scores the showdown, returning 1,
// 0.5 or 0 for the hero.
func playout(d *dealer, hero, villain []cards.Card, heroOnButton bool) float64 {
	bb, button := hero, villain
	if heroOnButton {
		bb, button = villain, hero
	}

	// The big blind's read is always one street stale; on the first draw
	// there is no read at all (table.Hand.OpponentDraw documents why).
	buttonDrew, known := 0, false
	for street := table.Draw1; street <= table.Draw3; street++ {
		bbDiscards := policy.DrawDiscards(bb, street,
			policy.Read{Count: buttonDrew, StreetsAgo: 1, Known: known})
		replace(bb, bbDiscards, d)

		btnDiscards := policy.DrawDiscards(button, street,
			policy.Read{Count: len(bbDiscards), StreetsAgo: 0, Known: true})
		replace(button, btnDiscards, d)
		buttonDrew, known = len(btnDiscards), true
	}

	heroValue, villainValue := deuce.Eval(hero), deuce.Eval(villain)
	switch {
	case heroValue > villainValue:
		return 1
	case heroValue == villainValue:
		return 0.5
	default:
		return 0
	}
}

// replace swaps each discarded card for a fresh one, in place.
func replace(hand []cards.Card, discards []cards.Card, d *dealer) {
	for _, out := range discards {
		for i := range hand {
			if hand[i] == out {
				hand[i] = d.deal()
				break
			}
		}
	}
}

// dealer deals without replacement from the 47 cards the hero does not
// hold, by partial Fisher-Yates over a reusable buffer. Discards are never
// recycled: two players drawing through three streets cannot exhaust 42
// cards, so the engine's reshuffle rule never comes into play heads-up.
type dealer struct {
	base []cards.Card
	deck []cards.Card
	used int
	rng  *rand.Rand
}

func newDealer(hero []cards.Card, rng *rand.Rand) *dealer {
	base := cards.NewSet(hero).Complement().Append(nil)
	return &dealer{base: base, deck: make([]cards.Card, len(base)), rng: rng}
}

func (d *dealer) reset() {
	copy(d.deck, d.base)
	d.used = 0
}

func (d *dealer) deal() cards.Card {
	swap := d.used + d.rng.Intn(len(d.deck)-d.used)
	d.deck[d.used], d.deck[swap] = d.deck[swap], d.deck[d.used]
	card := d.deck[d.used]
	d.used++
	return card
}
