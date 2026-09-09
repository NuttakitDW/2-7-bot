package cfr

import (
	"math"
	"testing"
)

func TestSeedBaseBlueprintIntoFixedGroups(t *testing.T) {
	previous := fixedProfile
	t.Cleanup(func() { fixedProfile = previous })
	for _, profile := range []string{"none", "early", "button"} {
		t.Run(profile, func(t *testing.T) {
			fixedProfile = profile
			tree := &Tree{Nodes: []Node{
				{Kind: KindBet, Street: Predraw, Actor: Btn, Acts: []uint8{Pass, Aggr}},
				{Kind: KindBet, Street: Draw1, Actor: Btn, Acts: []uint8{Pass, Aggr}},
				{Kind: KindBet, Street: Draw2, Actor: Btn, Acts: []uint8{Pass, Aggr}},
				{Kind: KindBet, Street: Draw1, Actor: BB, Acts: []uint8{Pass, Aggr}},
			}}
			a := &Abstraction{NumDrawClasses: 2, DrawBuckets: 4, Draw2Buckets: 4, FinalBuckets: 3}
			l := NewLayout(tree, a)
			base := l.WithoutFixed()
			if base.FixedGroups != 1 || l.FixedGroups != map[string]int{"none": 1, "early": 9, "button": 9}[profile] {
				t.Fatal("base layout mutated original groups")
			}
			bp := &Blueprint{Bet: make([]byte, base.BetSlots), Draw: make([]byte, base.DrawSlots)}
			for i := 0; i < len(bp.Bet); i += 2 {
				value := byte(1 + (i/2*73+17)%254)
				bp.Bet[i], bp.Bet[i+1] = value, 255-value
			}
			for i := 0; i < len(bp.Draw); i += MaxCand {
				value := byte(1 + (i/MaxCand*29+41)%254)
				bp.Draw[i], bp.Draw[i+1] = value, 255-value
			}
			clear(bp.Bet[:2])
			clear(bp.Draw[:MaxCand])
			tr := NewTrainer(tree, a, l, nil)
			if err := tr.SeedBlueprint(bp, 1000); err != nil {
				t.Fatal(err)
			}
			got := tr.Extract(1)
			check := func(source, exported []byte, regret []float64) bool {
				for i, b := range source {
					if math.Abs(regret[i]/1000-float64(b)/255) > 1e-12 || math.Abs(float64(exported[i])-float64(b)) > float64(len(source)-1) {
						return false
					}
				}
				return true
			}
			for group := 0; group < l.FixedGroups; group++ {
				for i := range tree.Nodes {
					node := &tree.Nodes[i]
					street := int(node.Street)
					for _, ctx := range []int{0, BetContexts(street) - 1} {
						for _, bucket := range []int{0, l.Buckets(street) - 1} {
							x := base.BetSlotFixed(node, ctx, bucket, group)
							y := l.BetSlotFixed(node, ctx, bucket, group)
							if !check(bp.Bet[x:x+2], got.Bet[y:y+2], tr.BetRegret[y:y+2]) {
								t.Fatalf("bet node %d group %d lost prior", i, group)
							}
						}
					}
				}
				for street := Draw1; street <= Draw3; street++ {
					for seat := 0; seat < 2; seat++ {
						for _, ctx := range []int{0, 15} {
							x := base.DrawSlotFixed(street, seat, 0, ctx, 0, group)
							y := l.DrawSlotFixed(street, seat, 0, ctx, 0, group)
							if !check(bp.Draw[x:x+MaxCand], got.Draw[y:y+MaxCand], tr.DrawRegret[y:y+MaxCand]) {
								t.Fatalf("draw %d seat %d group %d lost prior", street, seat, group)
							}
						}
					}
				}
			}
		})
	}
}
