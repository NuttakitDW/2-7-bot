package onyx

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func finalDrawTracker(t testing.TB) (*riverSolver, []cards.Card) {
	t.Helper()
	s, hand := riverTracker(t, 0)
	s.reset()
	s.observe(wire.Event{Kind: wire.EventDealHole, Seat: 0, Cards: hand}, 0)
	for {
		n := &s.tree.Nodes[s.node]
		if n.Kind == cfr.KindDraw && n.Street == cfr.Draw3 && n.Actor == cfr.Btn {
			return s, hand
		}
		if n.Kind == cfr.KindDraw {
			s.observe(wire.Event{Kind: wire.EventDrawResult, Seat: int(n.Actor), Count: 0}, 0)
		} else {
			a := wire.Check()
			if n.Facing {
				a = wire.Call()
			}
			s.observe(wire.Event{Kind: wire.EventActed, Seat: int(n.Actor), Action: a}, 0)
		}
	}
}

func TestLastDrawPlannerProtectsPatValueAndScope(t *testing.T) {
	s, hand := finalDrawTracker(t)
	s.model = &cfr.Empirical{Version: 1}
	for i := range s.model.Betting {
		s.model.Betting[i] = []cfr.PolicyNode{{Feature: -1, Prob: [6]float64{0, 1}}}
	}
	for i := range s.model.Drawing {
		s.model.Drawing[i] = []cfr.PolicyNode{{Feature: -1, Prob: [6]float64{1}}}
	}
	d := wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5}
	baseline := wire.Discard([]cards.Card{hand[4]})
	a := s.planLastDraw(0, hand, d, baseline)
	if a.Kind != wire.ActionDiscard || len(a.Cards) != 0 {
		t.Fatalf("broke profitable pat hand %+v", a)
	}
	for _, tt := range []struct {
		hero int
		kind string
	}{{1, wire.DecisionDraw}, {0, wire.DecisionWager}} {
		got := s.planLastDraw(tt.hero, hand, wire.Decision{Kind: tt.kind}, baseline)
		if len(got.Cards) != 1 || got.Cards[0] != hand[4] {
			t.Fatal("planned outside scope")
		}
	}
}

func TestPlannedRiverConditionsOnObservedReplacement(t *testing.T) {
	s, _ := riverTracker(t, 0)
	old := cards.MustParse("2c", "3d", "4h", "5s", "9c")
	hand := cards.MustParse("2c", "3d", "4h", "5s", "7c")
	s.planKnown = cards.NewSet(old)
	s.known = s.planKnown | cards.NewSet(hand)
	s.planCount = 1
	s.drawn[cfr.Btn][cfr.Draw3] = 1
	s.planBelief = []cfr.BeliefHand{
		{Hand: [5]cards.Card(cards.MustParse("2d", "3h", "4s", "5c", "7c")), Weight: .5},
		{Hand: [5]cards.Card(cards.MustParse("2h", "3s", "4c", "5d", "Kc")), Weight: .5},
	}
	s.model = &cfr.Empirical{Version: 1}
	s.model.Betting[3] = []cfr.PolicyNode{{Feature: -1, Prob: [6]float64{0, 1}}}
	d := wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: &wire.Range{MinTo: 200, MaxTo: 200}}
	if a, ok := s.mirrorFrom(0, hand, d, cfr.Draw1); !ok || a.Kind != wire.ActionBet {
		t.Fatalf("didn't follow the planned continuation %+v %v", a, ok)
	}
	s.known = s.planKnown
	v := s.view(&s.tree.Nodes[s.node])
	copy(v.Hand[:], old)
	if _, ok := s.plannedRiverAction(&v); ok {
		t.Fatal("used an unobserved replacement")
	}
	s.reset()
	if len(s.planBelief) > 0 || s.planKnown != 0 {
		t.Fatal("plan survived hand reset")
	}
}

func BenchmarkLastDrawPlanning(b *testing.B) {
	s, hand := finalDrawTracker(b)
	d := wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5}
	baseline := wire.Discard([]cards.Card{hand[4]})
	b.ResetTimer()
	for b.Loop() {
		s.planLastDraw(0, hand, d, baseline)
	}
}
