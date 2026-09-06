package cfr

import (
	"math"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

// ConditionRiverBelief updates a prior at the start of river betting using only
// subsequent public opponent bets. Earlier observations already informed the
// caller's root prior. Hero's known cards remove incompatible current hands.
func ConditionRiverBelief(m *Empirical, prior []BeliefHand, history []OpponentObservation, known cards.Set) []BeliefHand {
	return conditionRiverBelief(m, prior, history, known, .005)
}

// ConditionRiverBeliefRaw uses exactly the opponent likelihoods used by river
// continuation valuation. An observation with no model support returns nil.
func ConditionRiverBeliefRaw(m *Empirical, prior []BeliefHand, history []OpponentObservation, known cards.Set) []BeliefHand {
	return conditionRiverBelief(m, prior, history, known, 0)
}

func conditionRiverBelief(m *Empirical, prior []BeliefHand, history []OpponentObservation, known cards.Set, noise float64) []BeliefHand {
	if m == nil {
		return nil
	}
	out := make([]BeliefHand, 0, len(prior))
	total := 0.0
	for _, candidate := range prior {
		set := cards.NewSet(candidate.Hand[:])
		if set.Len() != 5 || set&known != 0 || candidate.Weight <= 0 || math.IsNaN(candidate.Weight) || math.IsInf(candidate.Weight, 0) {
			continue
		}
		for _, obs := range history {
			if obs.Draw || obs.View.Street != Draw3 {
				continue
			}
			if obs.Action < 0 || obs.Action > 2 {
				return nil
			}
			view := obs.View
			view.Hand = candidate.Hand
			p := m.betProbabilities(&view)
			candidate.Weight *= (1-noise)*p[obs.Action] + noise/3
		}
		total += candidate.Weight
		out = append(out, candidate)
	}
	if total <= 0 || math.IsNaN(total) || math.IsInf(total, 0) {
		return nil
	}
	for i := range out {
		out[i].Weight /= total
	}
	return out
}
