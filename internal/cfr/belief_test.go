package cfr

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

func passiveEmpirical() *Empirical {
	m := &Empirical{}
	for i := range m.Betting {
		m.Betting[i] = []PolicyNode{{Feature: -1, Prob: [6]float64{0, 1}}}
	}
	for i := range m.Drawing {
		m.Drawing[i] = []PolicyNode{{Feature: -1, Prob: [6]float64{1}}}
	}
	return m
}

func five(texts ...string) (out [5]cards.Card) {
	for i, s := range texts {
		out[i], _ = cards.ParseCard(s)
	}
	return
}

func TestRiverSolverMaximizesAcrossHiddenHands(t *testing.T) {
	tree := &Tree{Nodes: []Node{
		{Kind: KindBet, Street: 3, Actor: 0, Acts: []uint8{Pass, Aggr}, Next: [3]int32{1, 2}, Commit: [2]int32{100, 100}},
		{Kind: KindShowdown, Street: 3, Commit: [2]int32{100, 100}},
		{Kind: KindBet, Street: 3, Actor: 1, Facing: true, Acts: []uint8{Fold, Pass}, Next: [3]int32{4, 3}, Commit: [2]int32{200, 100}},
		{Kind: KindShowdown, Street: 3, Commit: [2]int32{200, 200}},
		{Kind: KindFold, Street: 3, Actor: 1, Commit: [2]int32{200, 100}},
	}}
	hero := five("2c", "3c", "4c", "7d", "9c")
	belief := []BeliefHand{{Hand: five("2d", "3d", "4d", "5d", "7s"), Weight: .75}, {Hand: five("2h", "3h", "4h", "7h", "Ks"), Weight: .25}}
	a, ev, ok := BestRiverAction(tree, 0, View{Hand: hero, Seat: 0, Street: 3}, belief, passiveEmpirical())
	if !ok || a != Pass || math.Abs(ev+50) > 1e-9 {
		t.Fatalf("action=%d EV=%v ok=%v; want check at -50", a, ev, ok)
	}
}

func TestOpponentBeliefConditionsOnActionsAndExcludesKnownCards(t *testing.T) {
	m := passiveEmpirical()
	m.Betting[0] = []PolicyNode{{Feature: 13, Threshold: 4, Left: 1, Right: 2}, {Feature: -1, Prob: [6]float64{0, 0, 1}}, {Feature: -1, Prob: [6]float64{0, 1}}}
	knownHand := five("2c", "3d", "4h", "7s", "Kc")
	known := cards.NewSet(knownHand[:])
	history := []OpponentObservation{{View: View{Seat: 1, Street: 0, Facing: true, CanRaise: true}, Action: Aggr}}
	got := OpponentBelief(m, history, known, rand.New(rand.NewPCG(11, 22)), 512)
	low := 0.0
	for _, p := range got {
		if cards.NewSet(p.Hand[:])&known != 0 || cards.NewSet(p.Hand[:]).Len() != 5 {
			t.Fatal("invalid sampled private cards")
		}
		if p.Hand[0].Rank <= cards.Four {
			low += p.Weight
		}
	}
	if low < .98 {
		t.Fatalf("posterior action evidence ignored: low fraction=%v", low)
	}
}
