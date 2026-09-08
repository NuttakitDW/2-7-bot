package cfr

import (
	"math"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

func TestOneCardRiverValueIncludesConditionalDrawsAndDeadCards(t *testing.T) {
	tree := BuildTree()
	var id int32 = -1
	for i, n := range tree.Nodes {
		if n.Kind == KindDraw && n.Street == Draw3 && n.Actor == Btn {
			id = int32(i)
			break
		}
	}
	if id < 0 {
		t.Fatal("missing last draw")
	}
	var hand, opponent [5]cards.Card
	copy(hand[:], cards.MustParse("2c", "3d", "4h", "5s", "9c"))
	copy(opponent[:], cards.MustParse("2d", "3h", "4s", "6c", "8d"))
	hero := View{Seat: Btn, Street: Draw3, Node: id, Hand: hand}
	good := cards.MustParse("7c")[0]
	bad := cards.MustParse("Tc")[0]
	available := cards.NewSet(opponent[:]) | cards.NewSet([]cards.Card{good, bad})
	var known cards.Set
	for i := 0; i < cards.DeckSize; i++ {
		c := cards.CardFromIndex(i)
		set := cards.NewSet([]cards.Card{c})
		if set&available == 0 {
			known |= set
		}
	}
	prior := []BeliefHand{{Hand: opponent, Weight: 1}}
	model := passiveEmpirical()
	pat, ok := OneCardRiverValue(tree, id, hero, prior, known, -1, model)
	root := &tree.Nodes[tree.Nodes[id].Next[0]]
	if want := -float64(root.Commit[Btn]); !ok || pat != want {
		t.Fatalf("pat %.2f %v want %.2f", pat, ok, want)
	}
	draw, ok := OneCardRiverValue(tree, id, hero, prior, known, 4, model)
	win := float64(root.Commit[BB] + BigBet)
	if want := (win + pat) / 2; !ok || math.Abs(draw-want) > 1e-9 {
		t.Fatalf("draw %.2f %v want %.2f", draw, ok, want)
	}
	prior[0].Dead = cards.NewSet([]cards.Card{bad})
	draw, ok = OneCardRiverValue(tree, id, hero, prior, known, 4, model)
	if !ok || draw != win {
		t.Fatalf("redrew hypothetical discard: %.2f %v want %.2f", draw, ok, win)
	}
	hero.Seat = BB
	if _, ok := OneCardRiverValue(tree, id, hero, prior, known, 4, model); ok {
		t.Fatal("planned before opponent's final draw")
	}
}

func TestOneCardRiverValueConditionsBeforeChoosingAction(t *testing.T) {
	tree := BuildTree()
	var id int32 = -1
	for i, n := range tree.Nodes {
		if n.Kind == KindDraw && n.Street == Draw3 && n.Actor == Btn {
			id = int32(i)
			break
		}
	}
	if id < 0 {
		t.Fatal("missing last draw")
	}
	var hero, strong, weak [5]cards.Card
	copy(hero[:], cards.MustParse("2c", "3d", "4h", "5s", "9c"))
	copy(strong[:], cards.MustParse("2d", "3h", "4s", "6c", "8d"))
	copy(weak[:], cards.MustParse("2d", "3h", "4s", "6c", "Kd"))
	available := cards.NewSet(strong[:]) | cards.NewSet(weak[:]) | cards.NewSet(cards.MustParse("7c", "Tc"))
	var known cards.Set
	for i := 0; i < cards.DeckSize; i++ {
		set := cards.NewSet([]cards.Card{cards.CardFromIndex(i)})
		if set&available == 0 {
			known |= set
		}
	}
	prior := []BeliefHand{{Hand: strong, Weight: .5}, {Hand: weak, Weight: .5}}
	v := View{Seat: Btn, Street: Draw3, Node: id, Hand: hero}
	got, ok := OneCardRiverValue(tree, id, v, prior, known, 4, passiveEmpirical())
	root := &tree.Nodes[tree.Nodes[id].Next[0]]
	win, loss := float64(root.Commit[BB]+BigBet), -float64(root.Commit[Btn])
	// 7c (1/3) beats both; 8d (1/6) reveals the weak hand and wins;
	// Kd (1/6) reveals the strong hand and loses. Tc (1/3) leaves an
	// even mixture: a bet gains against weak and loses against strong,
	// so its EV is zero, not the average of clairvoyant best responses.
	want := win/2 + loss/6
	if !ok || math.Abs(got-want) > 1e-9 {
		t.Fatalf("conditional draw EV %.9f %v want %.9f", got, ok, want)
	}
}
