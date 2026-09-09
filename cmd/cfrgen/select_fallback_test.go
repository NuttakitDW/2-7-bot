package main

import (
	"bytes"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cfr"
)

func TestFillMissingPolicyPreservesLearnedSets(t *testing.T) {
	w := newWorld()
	makeBP := func() *cfr.Blueprint {
		return &cfr.Blueprint{Bet: make([]byte, w.layout.BetSlots), Draw: make([]byte, w.layout.DrawSlots)}
	}
	bp, fallback := makeBP(), makeBP()
	var filled, learned [][]byte
	for group := 0; group < w.layout.FixedGroups; group++ {
		for i := range w.tree.Nodes {
			node := &w.tree.Nodes[i]
			if node.Kind != cfr.KindBet || len(node.Acts) < 2 || (group > 0 && !w.layout.Sliced(node, group)) {
				continue
			}
			n := int64(len(node.Acts))
			slot := w.layout.BetSlotFixed(node, 0, 0, group)
			fallback.Bet[slot], fallback.Bet[slot+1] = 100, 155
			filled = append(filled, bp.Bet[slot:slot+n])
			fallback.Bet[slot+n], fallback.Bet[slot+n+1] = 100, 155
			bp.Bet[slot+n+1] = 255
			learned = append(learned, bp.Bet[slot+n:slot+2*n])
			break
		}
	}
	for _, slot := range []int64{0, w.layout.DrawSlots - 2*cfr.MaxCand} {
		fallback.Draw[slot], fallback.Draw[slot+1] = 100, 155
		filled = append(filled, bp.Draw[slot:slot+cfr.MaxCand])
		fallback.Draw[slot+cfr.MaxCand], fallback.Draw[slot+cfr.MaxCand+1] = 100, 155
		bp.Draw[slot+cfr.MaxCand+1] = 255
		learned = append(learned, bp.Draw[slot+cfr.MaxCand:slot+2*cfr.MaxCand])
	}
	if err := fillMissingPolicy(w, bp, fallback); err != nil {
		t.Fatal(err)
	}
	for _, set := range filled {
		if !bytes.Equal(set[:2], []byte{100, 155}) {
			t.Fatalf("empty set did not retain fallback mixture: %v", set)
		}
	}
	for _, set := range learned {
		if !bytes.Equal(set[:2], []byte{0, 255}) {
			t.Fatalf("learned set changed or zero-probability action filled: %v", set)
		}
	}
	if fallback.Draw[0] != 100 || fallback.Draw[1] != 155 || bp.Draw[cfr.MaxCand*3] != 0 {
		t.Fatal("fallback changed or unsupported set acquired probability")
	}
}

func TestFillMissingPolicyRejectsMismatchedLayoutBeforeMutation(t *testing.T) {
	w := newWorld()
	bp := &cfr.Blueprint{Bet: []byte{0, 0}, Draw: []byte{0}}
	fallback := &cfr.Blueprint{Bet: []byte{255, 0}, Draw: []byte{255}}
	if err := fillMissingPolicy(w, bp, fallback); err == nil {
		t.Fatal("accepted tables that do not fit the layout")
	}
	if bp.Bet[0] != 0 || bp.Draw[0] != 0 {
		t.Fatal("mutated before validation")
	}
}
