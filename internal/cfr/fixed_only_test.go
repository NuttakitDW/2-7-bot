package cfr

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func TestFixedOnlyTrainingKeepsBaseRegretsAndLearnsKnownGroup(t *testing.T) {
	old := fixedProfile
	fixedProfile = "early"
	t.Cleanup(func() { fixedProfile = old })
	tree := &Tree{Nodes: []Node{
		{Kind: KindBet, Actor: Btn, Acts: []uint8{Pass, Aggr}, Next: [3]int32{1, 2}},
		{Kind: KindFold, Actor: Btn, Commit: [2]int32{50, 100}},
		{Kind: KindFold, Actor: BB, Commit: [2]int32{50, 100}},
	}}
	abs := BuildAbstraction()
	layout := NewLayout(tree, abs)
	// This synthetic game has no draws.
	layout.DrawSlots, layout.baseDraw, layout.earlyDraw = 0, 0, 0
	tr := NewTrainer(tree, abs, layout, nil)
	tr.FixedOnly = true
	bp := &Blueprint{Bet: make([]byte, layout.baseBet)}
	for i := 0; i < len(bp.Bet); i += 2 {
		bp.Bet[i], bp.Bet[i+1] = 128, 127
	}
	if err := tr.SeedBlueprint(bp, 1000); err != nil {
		t.Fatal(err)
	}
	before := append([]float64(nil), tr.BetRegret...)
	averageBefore := append([]float64(nil), tr.BetStrat...)
	visitsBefore := append([]uint32(nil), tr.BetVisits...)
	w := worker{tr: tr, rng: rand.New(rand.NewPCG(17, 1))}
	w.state.Deal(w.rng)
	w.walk(0, Btn, 1)
	if !slices.Equal(before, tr.BetRegret) {
		t.Fatal("unknown-card policy changed")
	}
	w.walk(0, BB, 10)
	if !slices.Equal(averageBefore, tr.BetStrat) || !slices.Equal(visitsBefore, tr.BetVisits) {
		t.Fatal("frozen average or visit counts changed")
	}
	w.state.FixedGroup = 1
	w.walk(0, Btn, 1)
	if !slices.Equal(before[:layout.baseBet], tr.BetRegret[:layout.baseBet]) {
		t.Fatal("base policy changed while training a known group")
	}
	w.walk(0, BB, 10)
	if !slices.Equal(averageBefore[:layout.baseBet], tr.BetStrat[:layout.baseBet]) {
		t.Fatal("base average changed")
	}
	if slices.Equal(averageBefore, tr.BetStrat) {
		t.Fatal("known-card average did not learn")
	}
	if slices.Equal(before, tr.BetRegret) {
		t.Fatal("known-card group did not learn")
	}
}

func TestFixedOnlyDrawingPreservesBaseAverage(t *testing.T) {
	tree := &Tree{Nodes: []Node{
		{Kind: KindDraw, Actor: Btn, Street: Draw1, Next: [3]int32{1}},
		{Kind: KindFold, Actor: BB, Commit: [2]int32{50, 100}},
	}}
	abs := BuildAbstraction()
	// A single draw class keeps this synthetic layout small.
	for i := range abs.Classes {
		abs.Classes[i].DrawClass = 0
	}
	base := int64(Streets-1) * 2 * aggrStates * drawCtx * MaxCand
	early := int64(2 * aggrStates * drawCtx * MaxCand)
	layout := &Layout{FixedGroups: 2, baseDraw: base, earlyDraw: early, DrawSlots: base + early, drawClasses: 1, early: true}
	tr := NewTrainer(tree, abs, layout, nil)
	tr.FixedOnly = true
	for i := range tr.DrawRegret {
		tr.DrawRegret[i] = 1
		tr.DrawStrat[i] = 2
	}
	before := append([]float64(nil), tr.DrawStrat...)
	w := worker{tr: tr, rng: rand.New(rand.NewPCG(18, 1))}
	w.state.Deal(w.rng)
	saved := w.state
	w.walk(0, BB, 10)
	if !slices.Equal(before, tr.DrawStrat) {
		t.Fatal("unknown-card draw average changed")
	}
	w.state = saved
	w.state.FixedGroup = 1
	w.walk(0, BB, 10)
	if !slices.Equal(before[:base], tr.DrawStrat[:base]) {
		t.Fatal("base draw average changed")
	}
	if slices.Equal(before, tr.DrawStrat) {
		t.Fatal("known-card draw average did not learn")
	}
}
