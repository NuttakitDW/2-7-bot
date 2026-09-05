package cfr

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/deuce"
)

func TestTargetedTrainingExportsStrategy(t *testing.T) {
	tree := BuildTree()
	abs := BuildAbstraction()
	layout := NewLayout(tree, abs)
	tr := NewTrainer(tree, abs, layout, deuce.NewTable())
	tr.Model, tr.ModelWeight = Heuristic{Cobalt: true}, 1
	tr.Run(40, 1, 123)
	bp := tr.Extract(1)
	for name, table := range map[string][]uint8{"bet": bp.Bet, "draw": bp.Draw} {
		found := false
		for _, p := range table {
			if p != 0 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("targeted training exported no %s strategy", name)
		}
	}
}

// A one-decision game with known values: passing loses 50, aggression
// wins 100. The fixed model always passes, so averaging its actions would
// export the wrong answer even though the learner finds the best action.
func TestTargetedTrainerLearnsKnownBestResponse(t *testing.T) {
	tree := &Tree{Nodes: []Node{
		{Kind: KindBet, Actor: Btn, Acts: []uint8{Pass, Aggr}, Next: [3]int32{1, 2}},
		{Kind: KindFold, Actor: Btn, Commit: [2]int32{50, 100}},
		{Kind: KindFold, Actor: BB, Commit: [2]int32{50, 100}},
	}}
	abs := BuildAbstraction()
	layout := NewLayout(tree, abs)
	tr := NewTrainer(tree, abs, layout, nil)
	tr.Model, tr.ModelWeight = passiveModel{}, 1
	w := worker{tr: tr, rng: rand.New(rand.NewPCG(17, 1))}
	w.state.Deal(w.rng)
	for i := 1; i <= 100; i++ {
		w.walk(0, Btn, float64(i))
		before := append([]float64(nil), tr.BetRegret...)
		w.averageOnly = true
		w.walk(0, BB, float64(i))
		if !slices.Equal(before, tr.BetRegret) {
			t.Fatal("averaging changed regrets")
		}
		w.averageOnly = false
	}
	pl := Player{Tree: tree, Abs: abs, Layout: layout, BP: tr.Extract(1)}
	v := w.state.View(tree, 0, Btn, w.rng)
	probs := pl.Probabilities(&v)
	if probs[1] < .99 {
		t.Fatalf("best action probability = %v, want >= .99", probs)
	}
}

type passiveModel struct{}

func (passiveModel) Bet(*View) (int, bool)    { return Pass, true }
func (passiveModel) Draw(*View) (uint8, bool) { return 31, true }

func TestConcurrentIterationsStopAtRequestedCount(t *testing.T) {
	tree := &Tree{Nodes: []Node{{Kind: KindFold, Actor: Btn}}}
	tr := NewTrainer(tree, &Abstraction{}, &Layout{}, nil)
	tr.Run(17, 4, 9)
	if got := tr.Iterations(); got != 17 {
		t.Fatalf("ran %d iterations, want 17", got)
	}
	tr.Run(20, 2, 9)
	if got := tr.Iterations(); got != 20 {
		t.Fatalf("resume ran %d iterations, want 20", got)
	}
}
