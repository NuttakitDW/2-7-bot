package cfr

import (
	"math"
	"slices"
	"testing"
)

func TestDiscountPreservesSignsAndVisitCounts(t *testing.T) {
	tr := &Trainer{BetRegret: []float64{12, -8}, DrawRegret: []float64{-4, 20}, BetStrat: []float64{2, 6}, DrawStrat: []float64{4, 8}, BetVisits: []uint32{7, 0}, DrawVisits: []uint32{8, 0}}
	tr.iterations.Store(123)
	if err := tr.Discount(.25); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tr.BetRegret, []float64{3, -2}) || !slices.Equal(tr.DrawRegret, []float64{-1, 5}) || !slices.Equal(tr.BetStrat, []float64{.5, 1.5}) || !slices.Equal(tr.DrawStrat, []float64{1, 2}) {
		t.Fatal("incorrect table discount")
	}
	if tr.Iterations() != 123 || tr.BetVisits[0] != 7 || tr.DrawVisits[0] != 8 {
		t.Fatal("discount changed observation counts")
	}
	for _, factor := range []float64{-1, 0, 1.1, math.NaN(), math.Inf(1)} {
		if err := tr.Discount(factor); err == nil {
			t.Errorf("accepted factor %v", factor)
		}
	}
}

func TestUniformAverageCountsEachSampleOnce(t *testing.T) {
	tree := &Tree{Nodes: []Node{
		{Kind: KindBet, Actor: Btn, Acts: []uint8{Pass, Aggr}, Next: [3]int32{1, 2}},
		{Kind: KindFold, Actor: Btn, Commit: [2]int32{50, 100}},
		{Kind: KindFold, Actor: BB, Commit: [2]int32{50, 100}},
	}}
	abs := BuildAbstraction()
	tr := NewTrainer(tree, abs, NewLayout(tree, abs), nil)
	tr.UniformAverage = true
	tr.Vanilla = true
	tr.Run(100, 1, 19)
	var mass float64
	var visits uint64
	for _, x := range tr.BetStrat {
		mass += x
	}
	for _, x := range tr.BetVisits {
		visits += uint64(x)
	}
	if visits == 0 || math.Abs(mass-float64(visits)) > 1e-9 {
		t.Fatalf("average mass %v, visits %v", mass, visits)
	}
}

func TestDiscountFixedOnlyPreservesBaseTables(t *testing.T) {
	tr := &Trainer{FixedOnly: true, Layout: &Layout{baseBet: 1, baseDraw: 1}, BetRegret: []float64{12, 8}, BetStrat: []float64{6, 4}, DrawRegret: []float64{10, 8}, DrawStrat: []float64{2, 4}}
	if err := tr.Discount(.5); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tr.BetRegret, []float64{12, 4}) || !slices.Equal(tr.BetStrat, []float64{6, 2}) || !slices.Equal(tr.DrawRegret, []float64{10, 4}) || !slices.Equal(tr.DrawStrat, []float64{2, 2}) {
		t.Fatal("discount changed frozen base tables or failed to discount known groups")
	}
}
