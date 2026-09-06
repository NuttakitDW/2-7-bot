package cfr

import (
	"path/filepath"
	"testing"
)

func TestSingleGroupStateReplicatesIntoFixedLayout(t *testing.T) {
	tree, abs := BuildTree(), BuildAbstraction()
	base := NewLayout(tree, abs)
	if base.FixedGroups != 1 {
		t.Skip("built with a fixed-card layout")
	}
	src := NewTrainer(tree, abs, base, nil)
	src.BetRegret[7], src.BetStrat[7], src.BetVisits[7] = 1.5, 2.5, 3
	src.DrawRegret[11], src.DrawStrat[11], src.DrawVisits[11] = 4.5, 5.5, 6
	src.iterations.Store(42)
	path := filepath.Join(t.TempDir(), "base.state")
	if err := src.SaveState(path); err != nil {
		t.Fatal(err)
	}
	fixed := *base
	fixed.FixedGroups = NumFixedGroups
	fixed.BetSlots *= NumFixedGroups
	fixed.DrawSlots *= NumFixedGroups
	dst := NewTrainer(tree, abs, &fixed, nil)
	if err := dst.LoadState(path); err != nil {
		t.Fatal(err)
	}
	if dst.Iterations() != 42 {
		t.Fatalf("iterations %d", dst.Iterations())
	}
	for group := 0; group < NumFixedGroups; group++ {
		b, d := fixed.GroupBase(group)
		if dst.BetRegret[b+7] != 1.5 || dst.BetStrat[b+7] != 2.5 || dst.BetVisits[b+7] != 3 {
			t.Fatalf("group %d bet slice not replicated", group)
		}
		if dst.DrawRegret[d+11] != 4.5 || dst.DrawStrat[d+11] != 5.5 || dst.DrawVisits[d+11] != 6 {
			t.Fatalf("group %d draw slice not replicated", group)
		}
	}
}
