package cfr

import "testing"

// The early profile must give every button predraw and first-draw set
// its own slot per group, inside the table, and share everything else.
func TestEarlyFixedLayoutSlicesOnlyEarlyButtonSets(t *testing.T) {
	old := fixedProfile
	fixedProfile = "early"
	t.Cleanup(func() { fixedProfile = old })
	tree, abs := BuildTree(), BuildAbstraction()
	l := NewLayout(tree, abs)
	if l.FixedGroups != NumFixedGroups {
		t.Fatalf("groups = %d", l.FixedGroups)
	}
	seen := map[int64]bool{}
	for i := range tree.Nodes {
		node := &tree.Nodes[i]
		if node.Kind != KindBet {
			continue
		}
		street := int(node.Street)
		for group := 0; group < NumFixedGroups; group++ {
			for ctx := 0; ctx < BetContexts(street); ctx++ {
				for bucket := 0; bucket < l.Buckets(street); bucket++ {
					slot := l.BetSlotFixed(node, ctx, bucket, group)
					base := l.BetSlot(node, ctx, bucket)
					if slot < 0 || slot+int64(len(node.Acts)) > l.BetSlots {
						t.Fatalf("node %d group %d slot %d outside %d", i, group, slot, l.BetSlots)
					}
					early := node.Actor == Btn && street <= fixedLastStreet
					if group > 0 && early != (slot != base) {
						t.Fatalf("node %d street %d actor %d group %d: sliced=%v", i, street, node.Actor, group, slot != base)
					}
					if group > 0 && early {
						if seen[slot] {
							t.Fatalf("slot %d shared between sliced sets", slot)
						}
						seen[slot] = true
					}
				}
			}
		}
	}
	for group := 1; group < NumFixedGroups; group++ {
		a := l.DrawSlotFixed(Draw1, Btn, 0, 0, 0, group)
		b := l.DrawSlotFixed(Draw2, Btn, 0, 0, 0, group)
		if a == l.DrawSlot(Draw1, Btn, 0, 0, 0) || b != l.DrawSlot(Draw2, Btn, 0, 0, 0) || a+MaxCand > l.DrawSlots {
			t.Fatalf("group %d draw slots %d/%d", group, a, b)
		}
		if l.DrawSlotFixed(Draw1, BB, 0, 0, 0, group) != l.DrawSlot(Draw1, BB, 0, 0, 0) {
			t.Fatal("the big blind's draws were sliced")
		}
	}
}
