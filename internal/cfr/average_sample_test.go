package cfr

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/handclass"
)

func averageSampleTrainer(draw bool) *Trainer {
	abs := &Abstraction{Classes: make([]ClassInfo, handclass.Num), NumDrawClasses: 1, FinalBuckets: 1, DrawBuckets: 1}
	for i := range abs.Classes {
		abs.Classes[i] = ClassInfo{NumCand: 1, Keep: [MaxCand]uint8{31}}
	}
	tree := &Tree{Nodes: []Node{
		{Kind: KindBet, Actor: Btn, Acts: []uint8{Pass, Aggr}, Next: [3]int32{1, 2}},
		{Kind: KindFold, Actor: BB, Commit: [2]int32{50, 100}},
		{Kind: KindFold, Actor: BB, Commit: [2]int32{50, 100}},
	}}
	if draw {
		tree.Nodes[0] = Node{Kind: KindDraw, Actor: Btn, Street: Draw1, Next: [3]int32{1}}
	}
	layout := NewLayout(tree, abs)
	if !draw {
		layout.DrawSlots = 0
	}
	tr := NewTrainer(tree, abs, layout, nil)
	tr.Model, tr.ModelWeight = passiveModel{}, 1
	for i := 0; i < len(tr.BetRegret); i += 2 {
		tr.BetRegret[i], tr.BetRegret[i+1] = 1, 3
	}
	for i := 0; i < len(tr.DrawRegret); i += MaxCand {
		tr.DrawRegret[i] = 1
	}
	return tr
}

func TestSampledAveragePreservesExpectedMass(t *testing.T) {
	const iterations = 20000
	for _, draw := range []bool{false, true} {
		for _, uniform := range []bool{false, true} {
			t.Run(fmt.Sprintf("draw=%t/uniform=%t", draw, uniform), func(t *testing.T) {
				tr := averageSampleTrainer(draw)
				tr.AverageEvery = 10
				tr.UniformAverage = uniform
				beforeBet, beforeDraw := slices.Clone(tr.BetRegret), slices.Clone(tr.DrawRegret)
				tr.Run(iterations, 1, 8173)
				var mass float64
				var visits uint64
				for _, values := range [][]float64{tr.BetStrat, tr.DrawStrat} {
					for _, value := range values {
						mass += value
					}
				}
				for _, values := range [][]uint32{tr.BetVisits, tr.DrawVisits} {
					for _, value := range values {
						visits += uint64(value)
					}
				}
				want := float64(iterations * (iterations + 1) / 2)
				if uniform {
					want = iterations
				}
				if math.Abs(mass/want-1) > 0.1 {
					t.Fatalf("weighted average mass %g, expected near %g", mass, want)
				}
				if visits < iterations/20 || visits > iterations/5 {
					t.Fatalf("expected sampled visit counts, got %d", visits)
				}
				if !slices.Equal(beforeBet, tr.BetRegret) || !slices.Equal(beforeDraw, tr.DrawRegret) {
					t.Fatal("equal-payoff actions changed regrets")
				}
			})
		}
	}
}

func TestAverageSamplingDefaultAndSelfPlayCompatibility(t *testing.T) {
	for _, model := range []bool{false, true} {
		baseline := averageSampleTrainer(false)
		if !model {
			baseline.Model, baseline.ModelWeight = nil, 0
		}
		baseline.Run(500, 1, 9197)
		for _, every := range []int{0, 1, 10} {
			if model && every == 10 {
				continue
			}
			tr := averageSampleTrainer(false)
			tr.AverageEvery = every
			if !model {
				tr.Model, tr.ModelWeight = nil, 0
			}
			tr.Run(500, 1, 9197)
			if !slices.Equal(tr.BetRegret, baseline.BetRegret) || !slices.Equal(tr.BetStrat, baseline.BetStrat) || !slices.Equal(tr.BetVisits, baseline.BetVisits) {
				t.Fatalf("changed legacy trajectory: model=%t every=%d", model, every)
			}
		}
	}
}
