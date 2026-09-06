package cfr

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

func TestRiverContinuationAveragesHiddenHandsBeforeHeroActs(t *testing.T) {
	tree := BuildTree()
	var id int32 = -1
	for i, n := range tree.Nodes {
		if n.Kind == KindBet && n.Street == Draw3 && n.Wagers == 0 && n.Actor == BB {
			id = int32(i)
			break
		}
	}
	if id < 0 {
		t.Fatal("no river root")
	}
	var nuts, catcher [5]cards.Card
	copy(nuts[:], cards.MustParse("2c", "3d", "4h", "5s", "7c"))
	copy(catcher[:], cards.MustParse("2d", "3h", "4s", "6c", "9d"))
	model := &Empirical{Version: 1}
	model.Betting[3] = []PolicyNode{{Feature: -1, Prob: [6]float64{0, 1, 0}}}
	hero := View{Node: id, Seat: Btn, Street: Draw3, Hand: nuts}
	prior := []BeliefHand{{Hand: catcher, Weight: 1}}
	ev, ok := RiverContinuationValue(tree, id, hero, prior, model)
	if want := float64(tree.Nodes[id].Commit[BB] + BigBet); !ok || ev != want {
		t.Fatalf("continuation EV %.2f %v want %.2f", ev, ok, want)
	}
	if _, _, ok := BestRiverAction(tree, id, hero, prior, model); ok {
		t.Fatal("action API answered for wrong actor")
	}
	hero.Hand = catcher
	prior[0].Hand = nuts
	ev, ok = RiverContinuationValue(tree, id, hero, prior, model)
	if want := -float64(tree.Nodes[id].Commit[Btn]); !ok || ev != want {
		t.Fatalf("losing continuation %.2f %v want %.2f", ev, ok, want)
	}
}
