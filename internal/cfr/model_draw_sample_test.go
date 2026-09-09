package cfr

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestModelDrawSamplingNestedCounterfactualUpdates(t *testing.T) {
	t.Run("policy", func(t *testing.T) { testModelDrawSamplingNested(t, false, false) })
	t.Run("uniform", func(t *testing.T) { testModelDrawSamplingNested(t, true, false) })
}

func testModelDrawSamplingNested(t *testing.T, uniform, baseline bool) {
	low, high := .1, .9
	if uniform {
		low, high = .5, .5
	}
	tr := averageSampleTrainer(true)
	for i := range tr.Abs.Classes {
		tr.Abs.Classes[i].NumCand = 2
		tr.Abs.Classes[i].Keep[1] = 31
	}
	tr.Tree = &Tree{Nodes: []Node{
		{Kind: KindDraw, Actor: Btn, Street: Draw1, Next: [3]int32{1}},
		{Kind: KindDraw, Actor: Btn, Street: Draw2, Next: [3]int32{2}},
		{Kind: KindBet, Actor: Btn, Street: Draw2, Acts: []uint8{Pass, Aggr}, Next: [3]int32{3, 4}},
		{Kind: KindFold, Actor: Btn, Commit: [2]int32{50, 100}},
		{Kind: KindFold, Actor: BB, Commit: [2]int32{50, 100}},
	}}
	tr = NewTrainer(tr.Tree, tr.Abs, NewLayout(tr.Tree, tr.Abs), nil)
	tr.Model, tr.ModelWeight, tr.ModelByHand = passiveModel{}, 1, true
	tr.SampleModelDraws, tr.Vanilla = true, true
	tr.UniformModelDraws = uniform
	tr.UseDrawBaseline = baseline
	tr.Run(0, 1, 1)
	for i := 0; i < len(tr.DrawRegret); i += MaxCand {
		// Include an action with zero policy probability: exploration must
		// still estimate its counterfactual regret.
		tr.DrawRegret[i], tr.DrawRegret[i+1] = 0, 1
	}
	for i := 0; i < len(tr.BetRegret); i += 2 {
		tr.BetRegret[i], tr.BetRegret[i+1] = 1, 3
	}
	betBefore, drawBefore := slices.Clone(tr.BetRegret), slices.Clone(tr.DrawRegret)
	w := worker{tr: tr, model: tr.Model, fixedOpponent: true, rng: rand.New(rand.NewPCG(34791, 1))}
	w.state.Deal(w.rng)
	saved := w.state
	const trials = 1
	var utility, bet0, bet1, drawDelta float64
	for _, first := range []bool{false, true} {
		for _, second := range []bool{false, true} {
			for i := range tr.drawBaseline {
				tr.drawBaseline[i] = float64(i%7)*13 - 30
			}
			probability := 1.0
			source := &drawSampleSource{}
			for _, common := range []bool{first, second} {
				if common {
					source.values = append(source.values, 3<<51)
					probability *= high
				} else {
					source.values = append(source.values, 0)
					probability *= low
				}
			}
			w.rng = rand.New(source)
			utility += probability * w.walk(0, Btn, 1)
			if w.state != saved || w.drawSampleWeight != 0 {
				t.Fatal("sampled traversal did not restore state and prefix weight")
			}
			for j, value := range tr.BetRegret {
				if j%2 == 0 {
					bet0 += probability * (value - betBefore[j])
				} else {
					bet1 += probability * (value - betBefore[j])
				}
			}
			for j, value := range tr.DrawRegret {
				drawDelta += probability * (value - drawBefore[j])
			}
			copy(tr.BetRegret, betBefore)
			copy(tr.DrawRegret, drawBefore)
		}
	}
	// Two own draws with two candidates each produce four counterfactual
	// prefixes for the later betting set. Their own reach is excluded.
	for _, c := range []struct {
		name                 string
		got, want, tolerance float64
	}{
		{"utility", utility / trials, 62.5, 1e-8},
		{"bet0", bet0 / trials, -450, 1e-8},
		{"bet1", bet1 / trials, 150, 1e-8},
		{"draw", drawDelta / trials, 0, 1e-8},
	} {
		if math.Abs(c.got-c.want) > c.tolerance {
			t.Fatalf("%s: got %g want %g", c.name, c.got, c.want)
		}
	}
}

func TestModelDrawSamplingGate(t *testing.T) {
	tr := averageSampleTrainer(true)
	w := worker{tr: tr, fixedOpponent: true}
	tr.ModelByHand = true
	if w.sampleModelDraws() {
		t.Fatal("sampling enabled by default")
	}
	tr.SampleModelDraws = true
	if !w.sampleModelDraws() {
		t.Fatal("fixed opponent walk not sampled")
	}
	w.averageOnly = true
	if w.sampleModelDraws() {
		t.Fatal("average pass sampled")
	}
	w.averageOnly, w.fixedOpponent = false, false
	if w.sampleModelDraws() {
		t.Fatal("self-play walk sampled")
	}
	w.fixedOpponent, tr.ModelByHand = true, false
	if w.sampleModelDraws() {
		t.Fatal("decision mixture sampled")
	}
}

// Only the two draw proposals consume randomness in the synthetic walk.
type drawSampleSource struct {
	values []uint64
	index  int
}

func (s *drawSampleSource) Uint64() uint64 { value := s.values[s.index]; s.index++; return value }

type drawSensitiveModel struct{}

func (drawSensitiveModel) Bet(v *View) (int, bool) {
	if v.Drawn[1-v.Seat][Draw1] == 0 {
		return int(Pass), true
	}
	return int(Aggr), true
}
func (drawSensitiveModel) Draw(*View) (uint8, bool) { return 31, true }

func drawSamplePayoffTrainer(parent bool) *Trainer {
	old := averageSampleTrainer(true)
	for i := range old.Abs.Classes {
		old.Abs.Classes[i].NumCand = 2
		old.Abs.Classes[i].Keep[0] = 31
		old.Abs.Classes[i].Keep[1] = 0
	}
	tree := &Tree{Nodes: []Node{
		{Kind: KindDraw, Actor: Btn, Street: Draw1, Next: [3]int32{1}},
		{Kind: KindBet, Actor: BB, Street: Draw1, Acts: []uint8{Pass, Aggr}, Next: [3]int32{2, 3}},
		{Kind: KindFold, Actor: Btn, Commit: [2]int32{50, 100}},
		{Kind: KindFold, Actor: BB, Commit: [2]int32{50, 100}},
	}}
	if parent {
		tree.Nodes = append(tree.Nodes, Node{Kind: KindBet, Actor: Btn, Acts: []uint8{Pass, Aggr}, Next: [3]int32{0, 0}})
		tree.Root = 4
	}
	tr := NewTrainer(tree, old.Abs, NewLayout(tree, old.Abs), nil)
	tr.Model, tr.ModelWeight, tr.ModelByHand = drawSensitiveModel{}, 1, true
	tr.SampleModelDraws, tr.Vanilla = true, true
	for i := 0; i < len(tr.DrawRegret); i += MaxCand {
		tr.DrawRegret[i+1] = 1
	}
	for i := 0; i < len(tr.BetRegret); i += 2 {
		tr.BetRegret[i], tr.BetRegret[i+1] = 1, 3
	}
	return tr
}

func TestModelDrawSamplingDistinctActionValues(t *testing.T) {
	tr := drawSamplePayoffTrainer(false)
	w := worker{tr: tr, model: tr.Model, fixedOpponent: true, rng: rand.New(rand.NewPCG(35001, 1))}
	w.state.Deal(w.rng)
	before := slices.Clone(tr.DrawRegret)
	var utility float64
	var delta [MaxCand]float64
	for _, common := range []bool{false, true} {
		source := &drawSampleSource{values: []uint64{0, 0}}
		probability := .1
		if common {
			source.values[0] = 1 << 52
			probability = .9
		}
		w.rng = rand.New(source)
		utility += probability * w.walk(tr.Tree.Root, Btn, 1)
		for i, value := range tr.DrawRegret {
			delta[i%MaxCand] += probability * (value - before[i])
		}
		copy(tr.DrawRegret, before)
	}
	if math.Abs(utility-100) > 1e-9 || math.Abs(delta[0]+150) > 1e-9 || math.Abs(delta[1]) > 1e-9 {
		t.Fatalf("utility=%g regrets=%v", utility, delta)
	}
}

func TestModelDrawSamplingBetSiblings(t *testing.T) {
	testModelDrawSamplingBetSiblings(t, false)
}

func testModelDrawSamplingBetSiblings(t *testing.T, baseline bool) {
	tr := drawSamplePayoffTrainer(true)
	tr.UseDrawBaseline = baseline
	tr.Run(0, 1, 1)
	// Freeze child draw policies so both siblings use the same policy.
	tr.FixedOnly = true
	tr.Layout.baseDraw = tr.Layout.DrawSlots
	tr.Layout.baseBet = 0
	w := worker{tr: tr, model: tr.Model, fixedOpponent: true, rng: rand.New(rand.NewPCG(35002, 1))}
	w.state.Deal(w.rng)
	saved := w.state
	before := slices.Clone(tr.BetRegret)
	var utility float64
	var delta [2]float64
	for _, first := range []bool{false, true} {
		for _, second := range []bool{false, true} {
			for i := range tr.drawBaseline {
				tr.drawBaseline[i] = float64(i%7)*13 - 30
			}
			source := &drawSampleSource{}
			probability := 1.0
			for _, common := range []bool{first, second} {
				if common {
					source.values = append(source.values, 1<<52, 0)
					probability *= .9
				} else {
					source.values = append(source.values, 0, 0)
					probability *= .1
				}
			}
			w.rng = rand.New(source)
			utility += probability * w.walk(tr.Tree.Root, Btn, 1)
			if w.state != saved || w.drawSampleWeight != 0 {
				t.Fatal("sibling prefix or state leaked")
			}
			for i, value := range tr.BetRegret {
				delta[i%2] += probability * (value - before[i])
			}
			copy(tr.BetRegret, before)
		}
	}
	if math.Abs(utility-100) > 1e-9 || math.Abs(delta[0]) > 1e-9 || math.Abs(delta[1]) > 1e-9 {
		t.Fatalf("utility=%g regrets=%v", utility, delta)
	}
}

func TestModelDrawSamplingAverageCompatibility(t *testing.T) {
	for _, selfPlay := range []bool{false, true} {
		baseline := averageSampleTrainer(true)
		candidate := averageSampleTrainer(true)
		candidate.SampleModelDraws = true
		candidate.UseDrawBaseline = true
		if selfPlay {
			baseline.Model = nil
			baseline.ModelWeight = 0
			candidate.Model = nil
			candidate.ModelWeight = 0
			baseline.ModelByHand = true
			candidate.ModelByHand = true
		}
		baseline.Run(100, 1, 35003)
		candidate.Run(100, 1, 35003)
		if !slices.Equal(baseline.DrawRegret, candidate.DrawRegret) || !slices.Equal(baseline.DrawStrat, candidate.DrawStrat) || !slices.Equal(baseline.DrawVisits, candidate.DrawVisits) {
			t.Fatal("inactive gate changed trajectory")
		}
	}
	tr := drawSamplePayoffTrainer(false)
	w := worker{tr: tr, model: tr.Model, fixedOpponent: true, averageOnly: true, rng: rand.New(rand.NewPCG(35004, 1))}
	w.state.Deal(w.rng)
	before := slices.Clone(tr.DrawRegret)
	w.walk(tr.Tree.Root, Btn, 1)
	if !slices.Equal(before, tr.DrawRegret) {
		t.Fatal("average-only pass updated regrets")
	}
}
