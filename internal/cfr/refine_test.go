package cfr

import (
	"math"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/handclass"
)

func TestRichFinalAbstractionRefinesLegacy(t *testing.T) {
	base, rich := buildAbstraction(false), buildAbstraction(true)
	if base.FinalBuckets != 18 || rich.FinalBuckets != 139 {
		t.Fatalf("final bucket counts: %d, %d", base.FinalBuckets, rich.FinalBuckets)
	}
	for id, info := range rich.Classes {
		if handclass.Weight(handclass.ID(id)) == 0 {
			continue
		}
		old := base.Classes[id]
		if int(info.Final) >= rich.FinalBuckets || uint16(rich.finalParents[info.Final]) != old.Final {
			t.Fatalf("class %d has invalid refinement", id)
		}
		info.Final = old.Final
		if info != old {
			t.Fatalf("class %d changed outside river betting", id)
		}
	}
	a, _ := cards.Parse([]string{"7c", "6d", "4h", "3s", "2c"})
	b, _ := cards.Parse([]string{"7c", "6d", "5h", "4s", "2c"})
	if base.Bucket(Draw3, Class(a)) != base.Bucket(Draw3, Class(b)) || rich.Bucket(Draw3, Class(a)) == rich.Bucket(Draw3, Class(b)) {
		t.Fatal("different seven lows should split their legacy bucket")
	}
}

func TestRichCompactLayoutFitsEveryBucket(t *testing.T) {
	base := newCompactLayout(BuildTree(), buildAbstraction(false))
	tree := BuildTree()
	rich := newCompactLayout(tree, buildAbstraction(true))
	if base.BetSlots != 950703 || rich.BetSlots <= base.BetSlots || rich.DrawSlots != base.DrawSlots {
		t.Fatalf("unexpected dimensions: base=%+v rich=%+v", base, rich)
	}
	for _, node := range tree.Nodes {
		if node.Kind != KindBet {
			continue
		}
		end := rich.BetSlot(&node, BetContexts(int(node.Street))-1, rich.Buckets(int(node.Street))-1) + int64(len(node.Acts))
		if node.Offset < 0 || end > rich.BetSlots {
			t.Fatalf("node outside table: %+v", node)
		}
	}
}

func TestRefinementPreservesPolicyAndSplitsPrior(t *testing.T) {
	for name, layout := range map[string]func(*Tree, *Abstraction) *Layout{"compact": newCompactLayout, "history": newHistoryLayout} {
		t.Run(name, func(t *testing.T) { testRefinementTransfer(t, layout) })
	}
}

func testRefinementTransfer(t *testing.T, layout func(*Tree, *Abstraction) *Layout) {
	t.Helper()
	makeTrainer := func(rich bool) *Trainer {
		a := buildAbstraction(rich)
		tree := &Tree{Nodes: []Node{
			{Kind: KindBet, Street: Predraw, Acts: []uint8{Fold, Pass, Aggr}},
			{Kind: KindBet, Street: Draw3, Acts: []uint8{Pass, Aggr}},
			{Kind: KindBet, Street: Draw3, Acts: []uint8{Pass, Aggr}},
		}}
		l := layout(tree, a)
		l.DrawSlots = MaxCand // enough to test the unchanged draw table
		return NewTrainer(tree, a, l, nil)
	}
	old, refined := makeTrainer(false), makeTrainer(true)
	old.iterations.Store(1234)
	for i := range old.BetStrat {
		old.BetStrat[i] = float64(i + 1)
		old.BetRegret[i] = float64(i + 2)
		old.BetVisits[i] = 1000
	}
	old.DrawStrat[2], old.DrawRegret[2], old.DrawVisits[0] = 17, 19, 23
	// Even a sparsely visited parent remains an available inherited policy.
	for parent := 0; parent < NumFinalBuckets; parent++ {
		x := old.Layout.BetSlot(&old.Tree.Nodes[1], 0, parent)
		old.BetVisits[x] = 1
	}
	refineTables(old, refined)
	bp := refined.Extract(1)
	for b := range refined.Abs.finalParents {
		y := refined.Layout.BetSlot(&refined.Tree.Nodes[1], 0, b)
		if bp.Bet[y] == 0 && bp.Bet[y+1] == 0 {
			t.Fatal("sparse inherited policy was pruned")
		}
	}
	if refined.Iterations() != 1234 || refined.DrawStrat[2] != 17 || refined.DrawRegret[2] != 19 || refined.DrawVisits[0] != 23 {
		t.Fatal("lost iteration or draw state")
	}
	if refined.BetStrat[12] != old.BetStrat[12] {
		t.Fatal("predraw policy changed")
	}
	var total [NumFinalBuckets]float64
	for b, parent := range refined.Abs.finalParents {
		x := old.Layout.BetSlot(&old.Tree.Nodes[1], 7, int(parent))
		y := refined.Layout.BetSlot(&refined.Tree.Nodes[1], 7, b)
		if math.Abs(refined.BetStrat[y]/refined.BetStrat[y+1]-old.BetStrat[x]/old.BetStrat[x+1]) > 1e-12 {
			t.Fatal("inherited action probabilities changed")
		}
		total[parent] += refined.BetRegret[y]
	}
	for parent, sum := range total {
		x := old.Layout.BetSlot(&old.Tree.Nodes[1], 7, parent)
		// Some legacy buckets are unreachable and have no children.
		if sum != 0 && math.Abs(sum-old.BetRegret[x]) > 1e-8 {
			t.Fatal("prior mass was duplicated or lost")
		}
	}
}

func TestRichHistoryRetainsEveryPublicNode(t *testing.T) {
	base := newHistoryLayout(BuildTree(), buildAbstraction(false))
	tree := BuildTree()
	rich := newHistoryLayout(tree, buildAbstraction(true))
	if rich.BetSlots <= base.BetSlots || rich.DrawSlots != base.DrawSlots {
		t.Fatal("invalid refinement dimensions")
	}
	var end int64
	for _, node := range tree.Nodes {
		if node.Kind != KindBet {
			continue
		}
		if node.Offset != end {
			t.Fatal("full history nodes overlapped or left a gap")
		}
		end = rich.BetSlot(&node, BetContexts(int(node.Street))-1, rich.Buckets(int(node.Street))-1) + int64(len(node.Acts))
		if end > rich.BetSlots {
			t.Fatal("node exceeds table bounds")
		}
	}
	if end != rich.BetSlots {
		t.Fatal("unexpected trailing slots")
	}
}
