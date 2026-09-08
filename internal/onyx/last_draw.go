package onyx

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// RefineFinalDraw prices close one-card/pat choices after the other player has
// drawn. A positive model EV margin is required before changing the baseline.
func (b *Bot) RefineFinalDraw(d wire.Decision, baseline wire.Action) wire.Action {
	if modelSelection != "plan-draw" || b.bayes == nil {
		return baseline
	}
	return b.bayes.planLastDraw(b.Table.Hand.Seat, b.Table.Hand.Cards, d, baseline)
}

func (s *riverSolver) planLastDraw(hero int, hand []cards.Card, d wire.Decision, baseline wire.Action) wire.Action {
	if hero != cfr.Btn || s.node < 0 || d.Kind != wire.DecisionDraw || d.MaxDiscards < 1 || baseline.Kind != wire.ActionDiscard || len(baseline.Cards) > 1 || len(hand) != 5 {
		return baseline
	}
	node := &s.tree.Nodes[s.node]
	if node.Kind != cfr.KindDraw || node.Street != cfr.Draw3 || node.Actor != cfr.Btn || s.drawn[cfr.BB][cfr.Draw3] < 0 {
		return baseline
	}
	if deuce.Eval(hand).Class() != deuce.HighCard {
		return baseline
	}
	v := s.view(node)
	copy(v.Hand[:], cards.SortedByRank(hand))
	if v.Hand[4].Rank < cards.Nine {
		return baseline
	}
	discard := 4
	if len(baseline.Cards) == 1 {
		discard = -1
		for i, c := range v.Hand {
			if c == baseline.Cards[0] {
				discard = i
				break
			}
		}
		if discard < 0 {
			return baseline
		}
	}
	known := s.known | cards.NewSet(hand)
	belief := s.belief(hero, known)
	pat, patOK := cfr.OneCardRiverValue(s.tree, s.node, v, belief, known, -1, s.model)
	draw, drawOK := cfr.OneCardRiverValue(s.tree, s.node, v, belief, known, discard, s.model)
	if !patOK || !drawOK {
		return baseline
	}
	const margin = .1 * cfr.BigBet
	result := baseline
	if len(baseline.Cards) == 0 && draw > pat+margin {
		result = wire.Legalize(d, wire.Discard([]cards.Card{v.Hand[discard]}), hand)
	}
	if len(baseline.Cards) == 1 && pat > draw+margin {
		result = wire.Discard(nil)
	}
	// Preserve the same posterior for river play, so the continuation policy
	// used to price this draw is also the one that actually follows it.
	s.planBelief, s.planKnown, s.planCount = belief, known, len(result.Cards)
	return result
}

func (s *riverSolver) plannedRiverAction(v *cfr.View) (int, bool) {
	if len(s.planBelief) == 0 || v.Street != cfr.Draw3 || v.Seat != cfr.Btn {
		return 0, false
	}
	known := s.known | cards.NewSet(v.Hand[:])
	if (known&^s.planKnown).Len() != s.planCount || int(v.Drawn[cfr.Btn][cfr.Draw3]) != s.planCount {
		return 0, false
	}
	prior := make([]cfr.BeliefHand, 0, len(s.planBelief))
	for _, p := range s.planBelief {
		held := cards.NewSet(p.Hand[:])
		if (held|p.Dead)&known != 0 {
			continue
		}
		if s.planCount == 1 {
			remaining := cards.DeckSize - (s.planKnown | held | p.Dead).Len()
			if remaining <= 0 {
				continue
			}
			p.Weight /= float64(remaining)
		}
		prior = append(prior, p)
	}
	posterior := cfr.ConditionRiverBeliefRaw(s.model, prior, s.history, known)
	a, _, ok := cfr.BestRiverAction(s.tree, s.node, *v, posterior, s.model)
	return a, ok
}
