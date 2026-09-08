package onyx

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// modelSelection can defer uncertain fitted decisions to the blueprint.
var modelSelection = "mode"

func (b *Bot) MirrorPredraw(d wire.Decision) (wire.Action, bool) {
	if (modelSelection != "all-model" && modelSelection != "all-bets" && modelSelection != "all-response" && modelSelection != "plan-draw") || b.bayes == nil || b.Table.Hand.Street != cfr.Predraw {
		return wire.Action{}, false
	}
	return b.bayes.mirrorFrom(b.Table.Hand.Seat, b.Table.Hand.Cards, d, cfr.Predraw)
}

// MirrorDraw optionally uses the fitted draw policy while sharing the same
// public-history tracker as modeled betting.
func (b *Bot) MirrorDraw(d wire.Decision) (wire.Action, bool) {
	if (modelSelection != "draw-model" && modelSelection != "all-model" && modelSelection != "all-response" && modelSelection != "plan-draw") || b.bayes == nil {
		return wire.Action{}, false
	}
	return b.bayes.mirrorDraw(b.Table.Hand.Seat, b.Table.Hand.Cards, d)
}

func (s *riverSolver) mirrorDraw(hero int, hand []cards.Card, d wire.Decision) (wire.Action, bool) {
	if s.node < 0 || d.Kind != wire.DecisionDraw || len(hand) != 5 {
		return wire.Action{}, false
	}
	n := &s.tree.Nodes[s.node]
	if n.Kind != cfr.KindDraw || int(n.Actor) != hero || n.Street < cfr.Draw1 || n.Street > cfr.Draw3 {
		return wire.Action{}, false
	}
	v := s.view(n)
	copy(v.Hand[:], cards.SortedByRank(hand))
	keep, ok := s.model.Mode().Draw(&v)
	if !ok {
		return wire.Action{}, false
	}
	var discard []cards.Card
	for i, card := range v.Hand {
		if keep&(1<<i) == 0 {
			discard = append(discard, card)
		}
	}
	return wire.Legalize(d, wire.Discard(discard), hand), true
}

// MirrorPostdraw applies the acting-player model to postdraw betting.
func (b *Bot) MirrorPostdraw(d wire.Decision) (wire.Action, bool) {
	if b.bayes == nil {
		return wire.Action{}, false
	}
	return b.bayes.mirrorFrom(b.Table.Hand.Seat, b.Table.Hand.Cards, d, cfr.Draw1)
}

func (s *riverSolver) mirrorFrom(hero int, hand []cards.Card, d wire.Decision, firstStreet int) (wire.Action, bool) {
	if s.node < 0 || d.Kind != wire.DecisionWager || len(hand) != 5 {
		return wire.Action{}, false
	}
	n := &s.tree.Nodes[s.node]
	if n.Kind != cfr.KindBet || int(n.Street) < firstStreet || int(n.Actor) != hero || n.Facing != (d.Call != nil) {
		return wire.Action{}, false
	}
	v := s.view(n)
	copy(v.Hand[:], cards.SortedByRank(hand))
	var a int
	var ok bool
	if len(s.planBelief) > 0 {
		a, ok = s.plannedRiverAction(&v)
	}
	if !ok && s.equilibrium != nil {
		a, ok = s.equilibrium.action(&v, s.rng.Float64())
	}
	if !ok && s.priors != nil {
		a, ok = s.rangeResponse(&v)
	}
	if !ok && modelSelection == "confident" {
		a, ok = s.model.ModeBet(&v, .75)
	} else if !ok {
		a, ok = s.model.Mode().Bet(&v)
	}
	if !ok {
		return wire.Action{}, false
	}
	if a != cfr.Aggr && s.riverRange != nil {
		if response, valid := s.riverRange.call(&v); valid {
			a = response
		}
	}
	action := wire.Check()
	switch a {
	case cfr.Fold:
		action = wire.Fold()
	case cfr.Aggr:
		action = wire.Raise(0)
	case cfr.Pass:
		if d.Call != nil {
			action = wire.Call()
		}
	}
	return wire.Legalize(d, action, hand), true
}
