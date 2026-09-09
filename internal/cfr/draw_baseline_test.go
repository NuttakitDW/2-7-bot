package cfr

import (
	"math"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

func TestDrawBaselineEstimates(t *testing.T) {
	for _, uniform := range []bool{false, true} {
		for _, exact := range []bool{false, true} {
			tr := drawSamplePayoffTrainer(false)
			tr.UseDrawBaseline, tr.UniformModelDraws = true, uniform
			tr.Run(0, 1, 1) // Allocate the optional cache without traversing.
			b := [2]float64{17, -23}
			if exact {
				b = [2]float64{-50, 100}
			}
			before := slices.Clone(tr.DrawRegret)
			var utility float64
			var delta [MaxCand]float64
			for a := 0; a < 2; a++ {
				for i := 0; i < len(tr.drawBaseline); i += MaxCand {
					tr.drawBaseline[i], tr.drawBaseline[i+1] = b[0], b[1]
				}
				w := worker{tr: tr, model: tr.Model, fixedOpponent: true, rng: rand.New(rand.NewPCG(35001, 1))}
				w.state.Deal(w.rng)
				info := &tr.Abs.Classes[Class(w.state.Hands[Btn][:])]
				slot := tr.Layout.DrawSlotFixed(Draw1, Btn, AggrState(Btn, w.state.LastAggr), DrawContext(Btn, Draw1, &w.state.Drawn), int(info.DrawClass), w.state.FixedGroup)
				source := &drawSampleSource{values: []uint64{0, 0}}
				q := .1
				if a == 1 {
					source.values[0] = 3 << 51
					q = .9
				}
				if uniform {
					q = .5
				}
				w.rng = rand.New(source)
				got := w.walk(tr.Tree.Root, Btn, 1)
				utility += q * got
				if exact && math.Abs(got-100) > 1e-9 {
					t.Fatalf("exact baseline retained action-selection variance: %g", got)
				}
				child := -50.0
				if a == 1 {
					child = 100
				}
				want := b[a] + .1*(child-b[a])
				if math.Abs(tr.drawBaseline[slot+int64(a)]-want) > 1e-9 {
					t.Fatal("EMA target must be child before current importance correction")
				}
				if tr.drawBaseline[slot+int64(1-a)] != b[1-a] {
					t.Fatal("updated unsampled baseline")
				}
				for i, v := range tr.DrawRegret {
					delta[i%MaxCand] += q * (v - before[i])
				}
				copy(tr.DrawRegret, before)
			}
			if math.Abs(utility-100) > 1e-9 || math.Abs(delta[0]+150) > 1e-9 || math.Abs(delta[1]) > 1e-9 {
				t.Fatalf("uniform=%v exact=%v utility=%g delta=%v", uniform, exact, utility, delta)
			}
		}
	}
}

func TestDrawBaselineResumeAndConcurrentWalks(t *testing.T) {
	tr := drawSamplePayoffTrainer(false)
	tr.UseDrawBaseline = true
	tr.Run(100, 4, 39001)
	if len(tr.drawBaseline) != len(tr.DrawRegret) {
		t.Fatal("cache not allocated")
	}
	path := filepath.Join(t.TempDir(), "state")
	if err := tr.SaveState(path); err != nil {
		t.Fatal(err)
	}
	for i := range tr.drawBaseline {
		tr.drawBaseline[i] = 123
	}
	if err := tr.LoadState(path); err != nil {
		t.Fatal(err)
	}
	for _, v := range tr.drawBaseline {
		if v != 0 {
			t.Fatal("resume retained stale baseline")
		}
	}
}

func TestDrawBaselineNestedAndSiblings(t *testing.T) {
	t.Run("nested-policy", func(t *testing.T) { testModelDrawSamplingNested(t, false, true) })
	t.Run("nested-uniform", func(t *testing.T) { testModelDrawSamplingNested(t, true, true) })
	t.Run("siblings", func(t *testing.T) { testModelDrawSamplingBetSiblings(t, true) })
}

func TestDrawBaselineZeroCompatibility(t *testing.T) {
	a, b := drawSamplePayoffTrainer(false), drawSamplePayoffTrainer(false)
	b.UseDrawBaseline = true
	b.Run(0, 1, 1)
	for _, tr := range []*Trainer{a, b} {
		w := worker{tr: tr, model: tr.Model, fixedOpponent: true, rng: rand.New(rand.NewPCG(49001, 1))}
		w.state.Deal(w.rng)
		w.walk(tr.Tree.Root, Btn, 1)
	}
	if !slices.Equal(a.DrawRegret, b.DrawRegret) {
		t.Fatal("zero baseline changed estimates")
	}
}

func TestDrawBaselineConcurrentInitialization(t *testing.T) {
	tr := drawSamplePayoffTrainer(false)
	tr.UseDrawBaseline = true
	path := filepath.Join(t.TempDir(), "state")
	if err := tr.SaveState(path); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); tr.Run(0, 1, 1) }()
	}
	if err := tr.LoadState(path); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}
