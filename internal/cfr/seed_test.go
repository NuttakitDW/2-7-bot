package cfr

import (
	"bytes"
	"math"
	"testing"
)

func TestSeedBlueprintPreservesPolicyAndUntrainedSets(t *testing.T) {
	tree := &Tree{Nodes: []Node{{Kind: KindBet, Actor: Btn, Acts: []uint8{Pass, Aggr}, Next: [3]int32{1, 2}}, {Kind: KindFold, Actor: Btn}, {Kind: KindFold, Actor: BB}}}
	abs := BuildAbstraction()
	l := NewLayout(tree, abs)
	tr := NewTrainer(tree, abs, l, nil)
	bp := &Blueprint{Bet: make([]byte, l.BetSlots), Draw: make([]byte, l.DrawSlots)}
	bp.Bet[0], bp.Bet[1] = 51, 204
	bp.Draw[0], bp.Draw[1] = 204, 51
	if err := tr.SeedBlueprint(bp, 1000); err != nil {
		t.Fatal(err)
	}
	if math.Abs(tr.BetRegret[0]-200) > 1e-9 || math.Abs(tr.BetRegret[1]-800) > 1e-9 {
		t.Fatalf("prior regrets %v", tr.BetRegret[:2])
	}
	got := tr.Extract(1)
	if !bytes.Equal(got.Bet, bp.Bet) || !bytes.Equal(got.Draw, bp.Draw) {
		t.Fatal("seed changed action probabilities or populated untrained sets")
	}
	if tr.Iterations() != 0 || tr.BetVisits[0] != 1 || tr.BetVisits[2] != 0 {
		t.Fatal("wrong prior eligibility or iteration count")
	}
	// Non-binary-exact ratios retain the in-memory policy; exporting is a
	// fresh quantization and may move a byte of probability mass.
	bp.Bet[0], bp.Bet[1] = 2, 253
	if err := tr.SeedBlueprint(bp, 1000); err != nil {
		t.Fatal(err)
	}
	var sigma [2]float64
	matchRegrets(tr.BetRegret[:2], sigma[:])
	if math.Abs(sigma[0]-2.0/255) > 1e-12 {
		t.Fatalf("changed prior mix: %v", sigma)
	}
	got = tr.Extract(1)
	if math.Abs(float64(got.Bet[0])-2) > 1 || int(got.Bet[0])+int(got.Bet[1]) != 255 {
		t.Fatalf("excess quantization drift: %v", got.Bet[:2])
	}
	tr.iterations.Store(1)
	if err := tr.SeedBlueprint(bp, 1000); err == nil {
		t.Fatal("seeded a nonfresh trainer")
	}
}

func TestSeedBlueprintRejectsInvalidInput(t *testing.T) {
	tr := &Trainer{Layout: &Layout{BetSlots: 2, DrawSlots: 6}}
	bp := &Blueprint{Bet: make([]byte, 2), Draw: make([]byte, 6)}
	for _, strength := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if err := tr.SeedBlueprint(bp, strength); err == nil {
			t.Errorf("accepted strength %v", strength)
		}
	}
	if err := tr.SeedBlueprint(nil, 1); err == nil {
		t.Fatal("accepted nil blueprint")
	}
	if err := tr.SeedBlueprint(&Blueprint{}, 1); err == nil {
		t.Fatal("accepted wrong layout")
	}
}
