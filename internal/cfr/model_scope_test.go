package cfr

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func TestOpponentTypeStaysFixedDuringTraversal(t *testing.T) {
	tr := &Trainer{Model: passiveModel{}, ModelWeight: .5, ModelByHand: true}
	w := worker{tr: tr, rng: rand.New(rand.NewPCG(9021, 1))}
	selected := 0
	for i := 0; i < 1000; i++ {
		w.selectOpponent()
		first := w.useModel()
		if first {
			selected++
		}
		for j := 0; j < 30; j++ {
			if w.useModel() != first {
				t.Fatal("opponent changed inside a traversal")
			}
		}
		w.averageOnly = true
		if w.useModel() {
			t.Fatal("fixed model entered averaging pass")
		}
		w.averageOnly = false
	}
	if selected < 400 || selected > 600 {
		t.Fatalf("wrong opponent mixture: %d/1000", selected)
	}
}

func TestOpponentScopePreservesPureEndpoints(t *testing.T) {
	for _, weight := range []float64{0, 1} {
		makeTrainer := func(byHand bool) *Trainer {
			tree := &Tree{Nodes: []Node{{Kind: KindBet, Actor: Btn, Acts: []uint8{Pass, Aggr}, Next: [3]int32{1, 2}}, {Kind: KindFold, Actor: Btn, Commit: [2]int32{50, 100}}, {Kind: KindFold, Actor: BB, Commit: [2]int32{50, 100}}}}
			a := buildAbstraction(false)
			l := newHistoryLayout(tree, a)
			l.DrawSlots = MaxCand
			tr := NewTrainer(tree, a, l, nil)
			tr.Model = passiveModel{}
			tr.ModelWeight = weight
			tr.ModelByHand = byHand
			return tr
		}
		before, after := makeTrainer(false), makeTrainer(true)
		before.Run(80, 1, 37)
		after.Run(80, 1, 37)
		if !slices.Equal(before.BetRegret, after.BetRegret) || !slices.Equal(before.BetStrat, after.BetStrat) || !slices.Equal(before.BetVisits, after.BetVisits) {
			t.Fatalf("changed pure endpoint weight %g", weight)
		}
	}
}
