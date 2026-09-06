package onyx

import (
	"math"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestRiverCallValuePricesWinsTiesAndLosses(t *testing.T) {
	hero := cards.MustParse("2c", "3d", "4h", "5s", "7c")
	weak := cards.MustParse("2d", "3h", "4s", "5c", "8d")
	tied := cards.MustParse("2d", "3h", "4s", "5c", "7d")
	for _, tt := range []struct {
		name   string
		hand   []cards.Card
		belief []cfr.BeliefHand
		want   float64
	}{
		{"win", hero, []cfr.BeliefHand{{Hand: [5]cards.Card(weak), Weight: 2}}, 800},
		{"loss", weak, []cfr.BeliefHand{{Hand: [5]cards.Card(hero), Weight: 3}}, -200},
		{"tie", hero, []cfr.BeliefHand{{Hand: [5]cards.Card(tied), Weight: 4}}, 300},
		{"mixed", tied, []cfr.BeliefHand{{Hand: [5]cards.Card(weak), Weight: 1}, {Hand: [5]cards.Card(hero), Weight: 3}}, 425},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := riverCallValue(tt.hand, tt.belief, 800, 200)
			if !ok || math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("got %v,%v; want %v,true", got, ok, tt.want)
			}
		})
	}
	for _, belief := range [][]cfr.BeliefHand{nil, {{Weight: 0}}, {{Weight: -1}}, {{Weight: math.NaN()}}} {
		if _, ok := riverCallValue(hero, belief, 800, 200); ok {
			t.Fatal("accepted invalid belief")
		}
	}
}

func TestRiverCallOnlyAnswersFacingFinalBet(t *testing.T) {
	s, hand := riverTracker(t, 1)
	b := &Bot{bayes: s}
	if _, ok := s.call(1, hand, wire.Decision{Kind: wire.DecisionWager, Check: true}); ok {
		t.Fatal("answered a free check")
	}
	// BB bets, button raises, so BB now faces a final-street call.
	s.observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Raise(0)}, 1)
	s.observe(wire.Event{Kind: wire.EventActed, Seat: 0, Action: wire.Raise(0)}, 1)
	call := uint64(200)
	d := wire.Decision{Kind: wire.DecisionWager, Call: &call, Fold: true}
	a, ok := s.call(1, hand, d)
	if !ok || (a.Kind != wire.ActionFold && a.Kind != wire.ActionCall) {
		t.Fatalf("got %+v,%v", a, ok)
	}
	if _, ok := s.call(0, hand, d); ok {
		t.Fatal("answered for wrong seat")
	}
	s.node = -1
	if _, ok := s.call(1, hand, d); ok {
		t.Fatal("answered after lost tracking")
	}
	b.bayes = nil
	if _, ok := b.RiverCall(d); ok {
		t.Fatal("answered with disabled solver")
	}
}
