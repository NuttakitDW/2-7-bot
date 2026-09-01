package equity

import (
	"math/rand"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/policy"
	"github.com/nuttakit/2-7-bot/internal/table"
)

// DrawRule is a draw policy both seats of a rollout can play —
// policy.DrawDiscards' shape. The draw-table generator passes each round's
// candidate table back in here, so the fixed point iterates without the
// policy package changing underneath it.
type DrawRule func(hand []cards.Card, street int, read policy.Read) []cards.Card

// DrawSample is one hero draw decision inside a rolled-out deal: the hand
// class held at the decision, the context it was decided in, and the
// showdown outcome of every candidate keep played against the same villain.
//
// Outcomes is indexed exactly like policy.DrawCandidates of the held hand —
// which is a class property, ranks and flushness being all the candidate
// rules read — so samples aggregate per (Class, context, candidate).
type DrawSample struct {
	Class    handclass.ID
	Street   int
	Read     policy.Read
	Outcomes []float64
}

// DrawSamples rolls out full deals under rule (nil means the current
// policy.DrawDiscards) on the real information timeline, the hero
// alternating seats, and emits one sample per hero draw decision.
//
// Branching at the decision rather than conditioning on it is the point:
// the pre-history that produced the hand and the read is played out rather
// than assumed, every context is weighted by how often real play reaches
// it, and the candidates are scored against the same villain hand — a
// paired comparison that cancels most of the sampling variance.
func DrawSamples(rule DrawRule, deals int, seed int64, visit func(DrawSample)) {
	if deals <= 0 {
		panic("equity: DrawSamples needs a positive deal count")
	}
	if rule == nil {
		rule = policy.DrawDiscards
	}
	rng := rand.New(rand.NewSource(seed))
	d := newDealer(nil, rng)

	hero := make([]cards.Card, deuce.HandSize)
	villain := make([]cards.Card, deuce.HandSize)
	for i := 0; i < deals; i++ {
		d.reset()
		for j := range hero {
			hero[j] = d.deal()
		}
		for j := range villain {
			villain[j] = d.deal()
		}
		sampleDeal(d, rule, hero, villain, i%2 == 0, visit)
	}
}

// sampleDeal plays one deal through the three draws, branching into every
// candidate at each hero decision and following rule on the trunk.
func sampleDeal(d *dealer, rule DrawRule, hero, villain []cards.Card, heroOnButton bool, visit func(DrawSample)) {
	bb, btn := hero, villain
	if heroOnButton {
		bb, btn = villain, hero
	}

	buttonDrew, known := 0, false
	for street := table.Draw1; street <= table.Draw3; street++ {
		bbRead := policy.Read{Count: buttonDrew, StreetsAgo: 1, Known: known}
		if !heroOnButton {
			visit(branch(d, rule, hero, villain, street, bbRead, false))
		}
		bbDiscards := rule(bb, street, bbRead)
		replace(bb, bbDiscards, d)

		btnRead := policy.Read{Count: len(bbDiscards), StreetsAgo: 0, Known: true}
		if heroOnButton {
			visit(branch(d, rule, hero, villain, street, btnRead, true))
		}
		btnDiscards := rule(btn, street, btnRead)
		replace(btn, btnDiscards, d)
		buttonDrew, known = len(btnDiscards), true
	}
}

// branch scores every candidate keep from one decision point, restoring the
// hands and the deck between candidates so each faces the same villain.
func branch(d *dealer, rule DrawRule, hero, villain []cards.Card, street int, read policy.Read, heroOnButton bool) DrawSample {
	candidates := policy.DrawCandidates(hero)
	outcomes := make([]float64, len(candidates))

	heroSnap := append([]cards.Card(nil), hero...)
	villainSnap := append([]cards.Card(nil), villain...)
	deckSnap := d.snapshot()

	heroScratch := make([]cards.Card, len(hero))
	villainScratch := make([]cards.Card, len(villain))
	for k, keep := range candidates {
		copy(heroScratch, heroSnap)
		copy(villainScratch, villainSnap)
		d.restore(deckSnap)
		outcomes[k] = playCandidate(d, rule, heroScratch, villainScratch, street, keep, heroOnButton)
	}
	d.restore(deckSnap)
	return DrawSample{Class: handclass.Of(heroSnap), Street: street, Read: read, Outcomes: outcomes}
}

// playCandidate forces the hero's keep on the decision street, finishes the
// hand with rule on both seats, and scores the showdown for the hero.
func playCandidate(d *dealer, rule DrawRule, hero, villain []cards.Card, street int, keep []cards.Rank, heroOnButton bool) float64 {
	bb, btn := hero, villain
	if heroOnButton {
		bb, btn = villain, hero
	}

	heroDiscards := policy.Discards(hero, keep)
	replace(hero, heroDiscards, d)

	var buttonDrew int
	if heroOnButton {
		// The hero drew last, so the street is complete.
		buttonDrew = len(heroDiscards)
	} else {
		// The button still draws this street, and reads the hero's forced
		// count — a candidate is priced with the reaction it provokes.
		btnDiscards := rule(btn, street, policy.Read{Count: len(heroDiscards), StreetsAgo: 0, Known: true})
		replace(btn, btnDiscards, d)
		buttonDrew = len(btnDiscards)
	}

	for street++; street <= table.Draw3; street++ {
		bbDiscards := rule(bb, street, policy.Read{Count: buttonDrew, StreetsAgo: 1, Known: true})
		replace(bb, bbDiscards, d)
		btnDiscards := rule(btn, street, policy.Read{Count: len(bbDiscards), StreetsAgo: 0, Known: true})
		replace(btn, btnDiscards, d)
		buttonDrew = len(btnDiscards)
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

// dealerState is a resumable snapshot of the deck between candidate
// branches. The rng is deliberately not part of it: replacement cards vary
// between candidates, but the villain hand — the dominant variance — is
// shared, and the whole pass stays reproducible from its seed.
type dealerState struct {
	deck []cards.Card
	used int
}

func (d *dealer) snapshot() dealerState {
	return dealerState{deck: append([]cards.Card(nil), d.deck...), used: d.used}
}

func (d *dealer) restore(s dealerState) {
	copy(d.deck, s.deck)
	d.used = s.used
}
