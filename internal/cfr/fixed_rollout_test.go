package cfr

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestFixedOnlyLateBetSamplesFrozenContinuation(t *testing.T) {
	tree := &Tree{Nodes: []Node{
		{Kind: KindBet, Actor: Btn, Street: Draw2, Acts: []uint8{Pass, Aggr}, Next: [3]int32{1, 2}},
		{Kind: KindFold, Actor: Btn, Commit: [2]int32{50, 100}},
		{Kind: KindFold, Actor: BB, Commit: [2]int32{50, 100}},
	}}
	abs := BuildAbstraction()
	layout := NewLayout(tree, abs)
	layout.early = true
	layout.DrawSlots = 0
	tr := NewTrainer(tree, abs, layout, nil)
	tr.FixedOnly = true
	tr.FrozenRollouts = true
	tr.Model, tr.ModelWeight = passiveModel{}, 1
	for i := 0; i < len(tr.BetRegret); i += 2 {
		tr.BetRegret[i], tr.BetRegret[i+1] = 1, 3
	}
	before := append([]float64(nil), tr.BetRegret...)
	w := worker{tr: tr, model: tr.Model, rng: rand.New(rand.NewPCG(919, 1))}
	w.state.Hands[Btn] = five("2c", "3d", "4h", "7s", "Kc")
	var sum float64
	for i := 0; i < 10000; i++ {
		value := w.walk(0, Btn, 1)
		if value != -50 && value != 100 {
			t.Fatalf("frozen continuation was enumerated: %g", value)
		}
		sum += value
	}
	// 25% lose50, 75% win100; sampling must preserve this expectation.
	if math.Abs(sum/10000-62.5) > 3 {
		t.Fatalf("biased continuation average: %g", sum/10000)
	}
	tr.FrozenRollouts = false
	if got := w.walk(0, Btn, 1); got != 62.5 {
		t.Fatalf("disabled optimization changed enumeration: %g", got)
	}
	tr.FrozenRollouts = true
	tr.Layout.early = false
	if got := w.walk(0, Btn, 1); got != 62.5 {
		t.Fatalf("sampled a layout with trainable later streets: %g", got)
	}
	if !slices.Equal(before, tr.BetRegret) {
		t.Fatal("frozen regrets changed")
	}
}

func TestFixedOnlyLateDrawSamplesOneFrozenContinuation(t *testing.T) {
	tree := &Tree{Nodes: []Node{
		{Kind: KindDraw, Actor: Btn, Street: Draw2, Next: [3]int32{1}},
		{Kind: KindFold, Actor: BB, Commit: [2]int32{50, 100}},
	}}
	abs := BuildAbstraction()
	w := worker{rng: rand.New(rand.NewPCG(920, 1))}
	w.state.Deal(w.rng)
	hand := w.state.Hands[Btn]
	info := &abs.Classes[Class(hand[:])]
	if info.NumCand < 2 {
		t.Fatal("test requires multiple draw candidates")
	}
	info.DrawClass = 0
	slots := int64(Streets-1) * 2 * aggrStates * drawCtx * MaxCand
	layout := &Layout{FixedGroups: 2, early: true, baseDraw: slots, DrawSlots: slots, drawClasses: 1}
	tr := NewTrainer(tree, abs, layout, nil)
	tr.FixedOnly = true
	tr.FrozenRollouts = true
	w.tr = tr
	w.state.Drawn[Btn][Draw2] = -1
	w.walk(0, Btn, 1)
	if w.state.Drawn[Btn][Draw2] < 0 {
		t.Fatal("late draw restored every branch instead of sampling one")
	}
	for _, r := range tr.DrawRegret {
		if r != 0 {
			t.Fatal("frozen draw regret changed")
		}
	}
}
