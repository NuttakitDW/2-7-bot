package cfr

import (
	"math"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

func TestConditionRiverBeliefWeightsPublicBetAndBlocksKnownCards(t *testing.T) {
	var strong, weak [5]cards.Card
	copy(strong[:], cards.MustParse("2c", "3d", "4h", "5s", "7c"))
	copy(weak[:], cards.MustParse("2d", "3h", "4s", "5c", "Kc"))
	model := &Empirical{Version: 1}
	model.Betting[3] = []PolicyNode{{Feature: 17, Threshold: 9, Left: 1, Right: 2}, {Feature: -1, Prob: [6]float64{0, .1, .9}}, {Feature: -1, Prob: [6]float64{0, .9, .1}}}
	// Feature17 is the highest card rank. Strong value bets 90%, weak air 10%.
	history := []OpponentObservation{{View: View{Street: 3, Seat: 1, CanRaise: true}, Action: Aggr}}
	prior := []BeliefHand{{Hand: strong, Weight: .5}, {Hand: weak, Weight: .5}}
	result := ConditionRiverBelief(model, prior, history, 0)
	if len(result) != 2 || math.Abs(result[0].Weight-.9) > .003 {
		t.Fatalf("posterior %+v", result)
	}
	result = ConditionRiverBelief(model, prior, history, cards.NewSet([]cards.Card{weak[4]}))
	if len(result) != 1 || result[0].Hand != strong || result[0].Weight != 1 {
		t.Fatalf("blockers %+v", result)
	}
	history[0].View.Street = 2
	result = ConditionRiverBelief(model, prior, history, 0)
	if len(result) != 2 || result[0].Weight != .5 {
		t.Fatalf("double-conditioned earlier street %+v", result)
	}
}

func TestConditionRiverBeliefRawMatchesContinuationLikelihoods(t *testing.T) {
	var strong, weak [5]cards.Card
	copy(strong[:], cards.MustParse("2c", "3d", "4h", "5s", "7c"))
	copy(weak[:], cards.MustParse("2d", "3h", "4s", "5c", "Kc"))
	model := &Empirical{Version: 1}
	model.Betting[3] = []PolicyNode{{Feature: 17, Threshold: 9, Left: 1, Right: 2}, {Feature: -1, Prob: [6]float64{0, .1, .9}}, {Feature: -1, Prob: [6]float64{0, .9, .1}}}
	history := []OpponentObservation{{View: View{Street: 3, Seat: BB, CanRaise: true}, Action: Aggr}}
	prior := []BeliefHand{{Hand: strong, Weight: .5}, {Hand: weak, Weight: .5}}
	result := ConditionRiverBeliefRaw(model, prior, history, 0)
	if len(result) != 2 || math.Abs(result[0].Weight-.9) > 1e-12 {
		t.Fatalf("raw posterior %+v", result)
	}
	model.Betting[3] = []PolicyNode{{Feature: -1, Prob: [6]float64{0, 1, 0}}}
	if result := ConditionRiverBeliefRaw(model, prior, history, 0); result != nil {
		t.Fatalf("unsupported observation should fall back: %+v", result)
	}
	if result := ConditionRiverBelief(model, prior, history, 0); len(result) != 2 {
		t.Fatalf("smoothed prior should retain support: %+v", result)
	}
}
