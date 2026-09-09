package onyx

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func riverTracker(t testing.TB, hero int) (*riverSolver, []cards.Card) {
	t.Helper()
	s, err := newRiverSolver()
	if err != nil {
		t.Fatal(err)
	}
	// Tests using this tracker install the specific decision model they exercise.
	s.equilibrium = nil
	s.priors = nil
	s.reset()
	var hand []cards.Card
	for _, text := range []string{"2c", "3d", "4h", "7s", "9c"} {
		c, _ := cards.ParseCard(text)
		hand = append(hand, c)
	}
	s.observe(wire.Event{Kind: wire.EventDealHole, Seat: hero, Cards: hand}, hero)
	for {
		n := &s.tree.Nodes[s.node]
		if n.Kind == cfr.KindBet && n.Street == cfr.Draw3 && int(n.Actor) == hero {
			break
		}
		if n.Kind == cfr.KindDraw {
			s.observe(wire.Event{Kind: wire.EventDrawResult, Seat: int(n.Actor), Count: 0}, hero)
		} else {
			a := wire.Check()
			if n.Facing {
				a = wire.Call()
			}
			s.observe(wire.Event{Kind: wire.EventActed, Seat: int(n.Actor), Action: a}, hero)
		}
		if s.node < 0 {
			t.Fatal("lost valid passive history")
		}
	}
	return s, hand
}

func TestRiverTrackerUsesPublicHistoryForBothSeats(t *testing.T) {
	for hero := 0; hero < 2; hero++ {
		s, hand := riverTracker(t, hero)
		if s.known != cards.NewSet(hand) || len(s.history) < 4 {
			t.Fatalf("missing hero cards or history: %+v", s)
		}
		for _, obs := range s.history {
			if obs.View.Seat == hero || obs.View.Hand != [5]cards.Card{} {
				t.Fatal("private cards entered public history")
			}
			if obs.Draw && obs.View.Drawn[1-hero][obs.View.Street] != -1 {
				t.Fatal("draw count lookahead")
			}
		}
		window := &wire.Range{MinTo: 200, MaxTo: 200}
		a, ok := s.decide(hero, hand, wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: window})
		if !ok || (a.Kind != wire.ActionCheck && a.Kind != wire.ActionRaise) {
			t.Fatalf("river decision %+v,%v", a, ok)
		}
		s.reset()
		if s.known != 0 || len(s.history) != 0 || s.drawn[hero][3] != -1 {
			t.Fatal("history survived next hand")
		}
	}
}

func TestRiverTrackerIgnoresOpponentPrivateCardsAndFailsClosed(t *testing.T) {
	s, hand := riverTracker(t, 1)
	before := s.known
	s.observe(wire.Event{Kind: wire.EventShowdownShow, Seat: 0, Cards: hand}, 1)
	s.observe(wire.Event{Kind: wire.EventDealHole, Seat: 0, Cards: hand}, 1)
	if s.known != before {
		t.Fatal("opponent private cards consumed")
	}
	s.observe(wire.Event{Kind: wire.EventActed, Seat: 0, Action: wire.Check()}, 1)
	if s.node != -1 {
		t.Fatal("invalid actor did not disable tracking")
	}
	if _, ok := s.decide(1, hand, wire.Decision{Kind: wire.DecisionWager}); ok {
		t.Fatal("answered after losing track")
	}
}

func BenchmarkBayesianRiver(b *testing.B) {
	s, hand := riverTracker(b, 1)
	d := wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: &wire.Range{MinTo: 200, MaxTo: 200}}
	for b.Loop() {
		_, _ = s.decide(1, hand, d)
	}
}

func TestRiverMemoConfiguration(t *testing.T) {
	oldBits, oldAlpha := beliefMemoBits, riverResponseAlpha
	t.Cleanup(func() { beliefMemoBits, riverResponseAlpha = oldBits, oldAlpha })
	beliefMemoBits, riverResponseAlpha = "4", "3"
	solver, err := newRiverSolver()
	if err != nil {
		t.Fatal(err)
	}
	if solver.model.ResponseAlpha != 3 {
		t.Fatal("cache lost response sharpening")
	}
	for _, bits := range []string{"-1", "26", "bad"} {
		beliefMemoBits = bits
		if _, err := newRiverSolver(); err == nil {
			t.Fatalf("accepted memo bits %s", bits)
		}
	}
}
