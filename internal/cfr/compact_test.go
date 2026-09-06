package cfr

import "testing"

func TestCompactLayoutSharesOnlyMatchingBettingStates(t *testing.T) {
	a := BuildAbstraction()
	full := newHistoryLayout(BuildTree(), a)
	tree := BuildTree()
	l := newCompactLayout(tree, a)
	if l.BetSlots >= full.BetSlots/10 || l.DrawSlots != full.DrawSlots {
		t.Fatalf("unexpected compact dimensions: %+v vs %+v", l, full)
	}
	type key struct {
		street, actor uint8
		wagers        int32
		commit        [2]int32
		facing        bool
	}
	seen := map[int64]key{}
	for _, n := range tree.Nodes {
		if n.Kind != KindBet {
			continue
		}
		k := key{n.Street, n.Actor, n.Wagers, n.Commit, n.Facing}
		if old, ok := seen[n.Offset]; ok && old != k {
			t.Fatalf("incompatible shared sets: %+v vs %+v", old, k)
		}
		seen[n.Offset] = k
		end := l.BetSlot(&n, BetContexts(int(n.Street))-1, l.Buckets(int(n.Street))-1) + int64(len(n.Acts))
		if n.Offset < 0 || end > l.BetSlots {
			t.Fatalf("slot outside compact table")
		}
	}
}
