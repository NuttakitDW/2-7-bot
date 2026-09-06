package cfr

import (
	"math/rand/v2"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

func TestOpponentBeliefHoldingStartsEveryParticleWithTheCard(t *testing.T) {
	m := &Empirical{Version: 4}
	for i := range m.BettingForest {
		m.BettingForest[i] = [][]PolicyNode{{{Feature: -1, Prob: [6]float64{1, 1, 1}}}}
	}
	for i := range m.DrawingForest {
		m.DrawingForest[i] = [][]PolicyNode{{{Feature: -1, Prob: [6]float64{1, 1, 1, 1, 1, 1}}}}
	}
	held := cards.NewSet(cards.MustParse("Qh"))
	known := cards.NewSet(cards.MustParse("2c", "3d", "4h", "7s", "Kc"))
	rng := rand.New(rand.NewPCG(5, 6))
	belief := OpponentBeliefHolding(m, nil, known, held, rng, 256)
	if len(belief) != 256 {
		t.Fatalf("particles: %d", len(belief))
	}
	for _, b := range belief {
		set := cards.NewSet(b.Hand[:])
		if set&held == 0 {
			t.Fatalf("particle %v lacks the held card", b.Hand)
		}
		if set&known != 0 {
			t.Fatalf("particle %v holds a known card", b.Hand)
		}
		if set.Len() != 5 {
			t.Fatalf("particle %v repeats a card", b.Hand)
		}
	}
	// A draw must never bring the held card back once discarded.
	obs := []OpponentObservation{{View: View{Street: Draw1, Node: 0}, Draw: true, Action: 5}}
	for _, b := range OpponentBeliefHolding(m, obs, known, held, rng, 256) {
		if cards.NewSet(b.Hand[:])&held != 0 {
			t.Fatalf("particle %v redrew the held card", b.Hand)
		}
	}
	if OpponentBeliefHolding(m, nil, known, known, rng, 8) != nil {
		t.Fatal("a held card the hero holds must be rejected")
	}
}
