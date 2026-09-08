package onyx

import (
	"math"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// RiverCall compares calling with folding on the final street. It requires
// the Bayesian profile and its public-history tracker. A combining policy
// can retain its own raises and use this only for passive decisions.
func (b *Bot) RiverCall(d wire.Decision) (wire.Action, bool) {
	if b.bayes == nil {
		return wire.Action{}, false
	}
	a, ok := b.bayes.call(b.Table.Hand.Seat, b.Table.Hand.Cards, d)
	if !ok {
		return wire.Action{}, false
	}
	return wire.Legalize(d, a, b.Table.Hand.Cards), true
}

func (s *riverSolver) call(hero int, hand []cards.Card, d wire.Decision) (wire.Action, bool) {
	if s.node < 0 || d.Kind != wire.DecisionWager || d.Call == nil || len(hand) != 5 {
		return wire.Action{}, false
	}
	n := &s.tree.Nodes[s.node]
	if n.Kind != cfr.KindBet || n.Street != cfr.Draw3 || int(n.Actor) != hero || !n.Facing {
		return wire.Action{}, false
	}
	v := s.view(n)
	belief := s.belief(hero, s.known|cards.NewSet(hand))
	value, ok := riverCallValue(hand, belief, float64(v.Pot), float64(v.ToCall))
	if !ok {
		return wire.Action{}, false
	}
	if value >= 0 {
		return wire.Call(), true
	}
	return wire.Fold(), true
}

// Return incremental chips relative to folding. Calling closes the
// heads-up final betting round, so no opponent response model is needed.
func riverCallValue(hand []cards.Card, belief []cfr.BeliefHand, pot, call float64) (float64, bool) {
	if len(hand) != 5 || call <= 0 || pot < call || math.IsNaN(pot+call) || math.IsInf(pot+call, 0) {
		return 0, false
	}
	hero := deuce.Eval(hand)
	win, mass := 0.0, 0.0
	for _, p := range belief {
		if p.Weight < 0 || math.IsNaN(p.Weight) || math.IsInf(p.Weight, 0) {
			return 0, false
		}
		if p.Weight == 0 {
			continue
		}
		value := deuce.Eval(p.Hand[:])
		if hero > value {
			win += p.Weight
		} else if hero == value {
			win += .5 * p.Weight
		}
		mass += p.Weight
	}
	if mass <= 0 || math.IsInf(mass, 0) {
		return 0, false
	}
	return win/mass*(pot+call) - call, true
}
