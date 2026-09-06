package cfr

import (
	"math"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/handclass"
)

func TestDrawingRefinementPreservesOtherHandInformation(t *testing.T) {
	base, rich := buildAbstraction(false), buildAbstraction(false)
	refineDrawingAbstraction(rich)
	if rich.DrawBuckets != 475 || rich.FinalBuckets != NumFinalBuckets {
		t.Fatalf("bucket counts %d,%d", rich.DrawBuckets, rich.FinalBuckets)
	}
	for id, info := range rich.Classes {
		if handclass.Weight(handclass.ID(id)) == 0 {
			continue
		}
		old := base.Classes[id]
		if int(info.Draw) >= rich.DrawBuckets || rich.drawParents[info.Draw] != old.Draw {
			t.Fatalf("invalid parent for %d", id)
		}
		info.Draw = old.Draw
		if info != old {
			t.Fatalf("changed other information for %d", id)
		}
	}
	tree := BuildTree()
	layout := newHistoryLayout(tree, rich)
	if layout.BetSlots != 68318159 {
		t.Fatalf("bet slots %d", layout.BetSlots)
	}
	var end int64
	for _, node := range tree.Nodes {
		if node.Kind != KindBet {
			continue
		}
		if node.Offset != end {
			t.Fatal("overlapping history nodes")
		}
		end = layout.BetSlot(&node, BetContexts(int(node.Street))-1, layout.Buckets(int(node.Street))-1) + int64(len(node.Acts))
		if end > layout.BetSlots {
			t.Fatal("invalid slot")
		}
	}
}

func TestDrawingHandProfileSelectsRefinement(t *testing.T) {
	prior := handProfile
	t.Cleanup(func() { handProfile = prior })
	handProfile = "draw-shape"
	if a := BuildAbstraction(); a.DrawBuckets != 475 {
		t.Fatal("profile did not refine drawing groups")
	}
}

func TestDrawingRefinementTransfersPrior(t *testing.T) {
	makeTrainer := func(rich bool) *Trainer {
		a := buildAbstraction(false)
		if rich {
			refineDrawingAbstraction(a)
		}
		tree := &Tree{Nodes: []Node{{Kind: KindBet, Street: Predraw, Acts: []uint8{Fold, Pass, Aggr}}, {Kind: KindBet, Street: Draw1, Acts: []uint8{Pass, Aggr}}, {Kind: KindBet, Street: Draw2, Acts: []uint8{Fold, Pass, Aggr}}, {Kind: KindBet, Street: Draw3, Acts: []uint8{Pass, Aggr}}}}
		l := newHistoryLayout(tree, a)
		l.DrawSlots = MaxCand
		return NewTrainer(tree, a, l, nil)
	}
	old, rich := makeTrainer(false), makeTrainer(true)
	old.iterations.Store(42)
	for i := range old.BetStrat {
		old.BetStrat[i] = float64(i + 1)
		old.BetRegret[i] = float64(i + 2)
		old.BetVisits[i] = 1
	}
	old.DrawStrat[1], old.DrawRegret[1], old.DrawVisits[0] = 2, 3, 4
	refineTables(old, rich)
	bp := rich.Extract(1)
	if rich.Iterations() != 42 || rich.DrawStrat[1] != 2 || rich.DrawRegret[1] != 3 || rich.DrawVisits[0] != 4 {
		t.Fatal("lost unchanged state")
	}
	for i, node := range rich.Tree.Nodes {
		street := int(node.Street)
		var total [NumDrawBuckets]float64
		for bucket := 0; bucket < rich.Layout.Buckets(street); bucket++ {
			parent := bucket
			if street == Draw1 || street == Draw2 {
				parent = int(rich.Abs.drawParents[bucket])
			}
			x := old.Layout.BetSlot(&old.Tree.Nodes[i], 0, parent)
			y := rich.Layout.BetSlot(&node, 0, bucket)
			if math.Abs(rich.BetStrat[y]/rich.BetStrat[y+1]-old.BetStrat[x]/old.BetStrat[x+1]) > 1e-12 {
				t.Fatal("changed inherited policy")
			}
			if bp.Bet[y] == 0 && bp.Bet[y+1] == 0 {
				t.Fatal("pruned inherited policy")
			}
			if street == Draw1 || street == Draw2 {
				total[parent] += rich.BetRegret[y]
			} else if rich.BetRegret[y] != old.BetRegret[x] {
				t.Fatal("changed another street")
			}
		}
		if street == Draw1 || street == Draw2 {
			for parent, sum := range total {
				x := old.Layout.BetSlot(&old.Tree.Nodes[i], 0, parent)
				if sum != 0 && math.Abs(sum-old.BetRegret[x]) > 1e-8 {
					t.Fatal("duplicated prior mass")
				}
			}
		}
	}
}

func TestDrawingRefinementPreservesExistingRichRiver(t *testing.T) {
	makeTrainer := func(refined bool) *Trainer {
		a := buildAbstraction(true)
		if refined {
			refineDrawingAbstraction(a)
		}
		tree := &Tree{Nodes: []Node{
			{Kind: KindBet, Street: Draw1, Acts: []uint8{Pass, Aggr}},
			{Kind: KindBet, Street: Draw3, Acts: []uint8{Pass, Aggr}},
		}}
		l := newCompactLayout(tree, a)
		l.DrawSlots = MaxCand
		return NewTrainer(tree, a, l, nil)
	}
	old, refined := makeTrainer(false), makeTrainer(true)
	for i := range old.BetStrat {
		old.BetStrat[i], old.BetRegret[i], old.BetVisits[i] = float64(i+1), float64(i+2), uint32(i+3)
	}
	refineTables(old, refined)
	for ctx := 0; ctx < BetContexts(Draw3); ctx++ {
		for bucket := 0; bucket < old.Abs.FinalBuckets; bucket++ {
			x := old.Layout.BetSlot(&old.Tree.Nodes[1], ctx, bucket)
			y := refined.Layout.BetSlot(&refined.Tree.Nodes[1], ctx, bucket)
			for action := int64(0); action < 2; action++ {
				if old.BetStrat[x+action] != refined.BetStrat[y+action] || old.BetRegret[x+action] != refined.BetRegret[y+action] {
					t.Fatalf("rich river prior changed at context %d bucket %d", ctx, bucket)
				}
			}
			if old.BetVisits[x] != refined.BetVisits[y] {
				t.Fatal("rich river visits changed")
			}
		}
	}
}
