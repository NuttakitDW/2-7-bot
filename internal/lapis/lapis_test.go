package lapis

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func newBot(t *testing.T, seats int) *Bot {
	t.Helper()
	b, err := New()
	if err != nil {
		t.Fatal(err)
	}
	b.Hello(wire.Message{GameID: "27td-fl", SeatCount: seats})
	b.HandStart(wire.Message{Seat: 0})
	b.Observe(wire.Event{Kind: wire.EventHandStart, Button: 0})
	b.Observe(wire.Event{Kind: wire.EventPost, Seat: 1, PostKind: "big-blind"})
	b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: 0})
	b.Observe(wire.Event{Kind: wire.EventDealHole, Seat: 0, Cards: cards.MustParse("7c", "5d", "4h", "3s", "2c")})
	return b
}

func acted(seat int, kind string) wire.Event {
	return wire.Event{Kind: wire.EventActed, Seat: seat, Action: wire.Action{Kind: kind}}
}

func TestTrackerFollowsTheTree(t *testing.T) {
	b := newBot(t, 2)
	if b.node != b.tree.Root {
		t.Fatalf("node = %d, want root %d", b.node, b.tree.Root)
	}
	b.Observe(acted(0, wire.ActionRaise))
	b.Observe(acted(1, wire.ActionCall))
	node := &b.tree.Nodes[b.node]
	if node.Kind != cfr.KindDraw || node.Street != cfr.Draw1 || node.Actor != cfr.BB {
		t.Fatalf("after raise-call: %+v", *node)
	}
	if b.lastAggr != 0 {
		t.Fatalf("lastAggr = %d", b.lastAggr)
	}
	b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: 1})
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 2})
	node = &b.tree.Nodes[b.node]
	if node.Kind != cfr.KindDraw || node.Actor != cfr.Btn || b.drawn[1][1] != 2 {
		t.Fatalf("after bb draw: %+v drawn %v", *node, b.drawn)
	}
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 0, Count: 0})
	node = &b.tree.Nodes[b.node]
	if node.Kind != cfr.KindBet || node.Actor != cfr.BB || node.Facing {
		t.Fatalf("after button draw: %+v", *node)
	}
	b.Observe(acted(1, wire.ActionCheck))
	node = &b.tree.Nodes[b.node]
	if node.Actor != cfr.Btn || node.Facing {
		t.Fatalf("after check: %+v", *node)
	}
	// Our turn, not facing: the proposal must be a check or a bet, and the
	// tracker must still be on the tree.
	action := b.Decide(wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: &wire.Range{MinTo: 100, MaxTo: 100}})
	if action.Kind != wire.ActionCheck && action.Kind != wire.ActionBet {
		t.Fatalf("action = %+v", action)
	}
}

func TestTrackerLosesOnAnUnexpectedEvent(t *testing.T) {
	b := newBot(t, 2)
	// The big blind cannot act before the button predraw.
	b.Observe(acted(1, wire.ActionRaise))
	if b.node != lost {
		t.Fatal("tracker should have given up")
	}
	call := uint64(50)
	action := b.Decide(wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call, Raise: &wire.Range{MinTo: 200, MaxTo: 200}})
	if b.Fallbacks != 1 || action.Kind != wire.ActionRaise {
		t.Fatalf("fallback = %d, action %+v", b.Fallbacks, action)
	}
}

func TestThreeSeatsAlwaysFallBack(t *testing.T) {
	b := newBot(t, 3)
	if b.node != lost {
		t.Fatal("a three-seat table has no tree")
	}
}

func TestDrawDiscardsComeFromTheHand(t *testing.T) {
	b := newBot(t, 2)
	b.Observe(acted(0, wire.ActionRaise))
	b.Observe(acted(1, wire.ActionCall))
	b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: 1})
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 2})
	for i := 0; i < 20; i++ {
		action := b.Decide(wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5})
		if action.Kind != wire.ActionDiscard {
			t.Fatalf("action = %+v", action)
		}
		for _, card := range action.Cards {
			if !cards.Contains(b.Table.Hand.Cards, card) {
				t.Fatalf("discarded %v, not held", card)
			}
		}
	}
}
