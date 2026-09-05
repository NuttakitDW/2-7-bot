package astra

import (
	"strings"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/table"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func riverState(t *testing.T, text string, opp int) *table.Table {
	t.Helper()
	s := table.New()
	s.HandStart(wire.Message{Seat: 0})
	s.Observe(wire.Event{Kind: wire.EventStreetStart, Street: table.Draw3})
	for _, word := range strings.Fields(text) {
		c, err := cards.ParseCard(word)
		if err != nil {
			t.Fatal(err)
		}
		s.Hand.Cards = append(s.Hand.Cards, c)
	}
	s.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: opp})
	return s
}

func TestRiverBluffMixAndPatRestraint(t *testing.T) {
	s := riverState(t, "2c 3d 4h 8s 8c", 1)
	d := wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: &wire.Range{MinTo: 200, MaxTo: 200}}
	for _, tc := range []struct {
		mix  float64
		want string
	}{{0.1, wire.ActionRaise}, {0.9, wire.ActionCheck}} {
		if got := propose(&s.Hand, d, tc.mix); got.Kind != tc.want {
			t.Fatalf("mix %v: got %s, want %s", tc.mix, got.Kind, tc.want)
		}
	}
	s.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
	if got := propose(&s.Hand, d, 0); got.Kind != wire.ActionCheck {
		t.Fatalf("bluffed pat opponent: %v", got)
	}
}

func TestRoughEightDoesNotCapRiver(t *testing.T) {
	s := riverState(t, "8c 7d 6h 3s 2c", 0)
	s.Hand.Wagers = 2
	call := uint64(200)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call, Raise: &wire.Range{MinTo: 600, MaxTo: 600}}
	if got := Decide(s, d); got.Kind != wire.ActionCall {
		t.Fatalf("rough eight facing raise: %v", got)
	}
}

func TestRiverValueAndCappedNuts(t *testing.T) {
	s := riverState(t, "9c 6d 4h 3s 2c", 1)
	d := wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: &wire.Range{MinTo: 200, MaxTo: 200}}
	if got := Decide(s, d); got.Kind != wire.ActionBet || got.To != 200 {
		t.Fatalf("nine should value bet a draw: %v", got)
	}
	s = riverState(t, "7c 5d 4h 3s 2c", 0)
	call := uint64(200)
	d = wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call}
	if got := Decide(s, d); got.Kind != wire.ActionCall {
		t.Fatalf("capped nuts: %v", got)
	}
}
