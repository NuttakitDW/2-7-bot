package lapis

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestEmbeddedPolicyHashMatchesPayload(t *testing.T) {
	want := sha256.Sum256(blueprintData)
	got, err := EmbeddedPolicyHash()
	if err != nil || got != hex.EncodeToString(want[:]) {
		t.Fatalf("hash=%q err=%v", got, err)
	}
}

type captureDrawModel struct {
	view  cfr.View
	keep  uint8
	rands []float64
}

func (m *captureDrawModel) Bet(*cfr.View) (int, bool) { return 0, false }
func (m *captureDrawModel) Draw(v *cfr.View) (uint8, bool) {
	m.view = *v
	m.rands = append(m.rands, v.Rand)
	return m.keep, true
}

func TestNewModelSeedHasReproducibleDrawMix(t *testing.T) {
	aModel, bModel := &captureDrawModel{keep: 31}, &captureDrawModel{keep: 31}
	a, b := NewModelSeed(aModel, 77), NewModelSeed(bModel, 77)
	node, _ := cfr.ProjectDrawNode(cfr.Draw1, cfr.Btn, cfr.PolicyHistory{}, -1)
	hand := cards.MustParse("2c", "3d", "4h", "7s", "Kc")
	for i := 0; i < 4; i++ {
		if _, ok := a.ProjectedDraw(node, cfr.Btn, hand, [2]cfr.DrawCounts{}, -1); !ok {
			t.Fatal("seeded model A rejected draw")
		}
		if _, ok := b.ProjectedDraw(node, cfr.Btn, hand, [2]cfr.DrawCounts{}, -1); !ok {
			t.Fatal("seeded model B rejected draw")
		}
	}
	if !reflect.DeepEqual(aModel.rands, bModel.rands) {
		t.Fatalf("same seed draw mixes differ: %v / %v", aModel.rands, bModel.rands)
	}
}

func TestProjectedDrawUsesLoadedModelWithSortedHand(t *testing.T) {
	model := &captureDrawModel{keep: 0b00111}
	b := NewModel(model)
	node, ok := cfr.ProjectDrawNode(cfr.Draw2, cfr.Btn, cfr.PolicyHistory{}, cfr.BB)
	if !ok {
		t.Fatal("missing projection")
	}
	hand := cards.MustParse("Kd", "2c", "7s", "4h", "3d")
	var drawn [2]cfr.DrawCounts
	for p := range drawn {
		for street := range drawn[p] {
			drawn[p][street] = -1
		}
	}
	action, ok := b.ProjectedDraw(node, cfr.Btn, hand, drawn, cfr.BB)
	if !ok || action.Kind != wire.ActionDiscard {
		t.Fatalf("action = %+v, ok=%v", action, ok)
	}
	wantDiscards := cards.MustParse("7s", "Kd")
	if !reflect.DeepEqual(action.Cards, wantDiscards) {
		t.Fatalf("discards = %v, want %v", action.Cards, wantDiscards)
	}
	if model.view.Node != node || model.view.Seat != cfr.Btn || model.view.Street != cfr.Draw2 || model.view.LastAggr != cfr.BB {
		t.Fatalf("view = %+v", model.view)
	}
	wantHand := cards.MustParse("2c", "3d", "4h", "7s", "Kd")
	if !reflect.DeepEqual(model.view.Hand[:], wantHand) || model.view.Drawn != drawn {
		t.Fatalf("view hand/draws = %v/%v, want %v/%v", model.view.Hand, model.view.Drawn, wantHand, drawn)
	}
	if model.view.Pot != 0 || model.view.ToCall != 0 || model.view.Wagers != 0 || model.view.Facing || model.view.CanRaise {
		t.Fatalf("draw carried betting fields: %+v", model.view)
	}
}

func TestProjectedDrawRejectsDuplicateOrWrongActor(t *testing.T) {
	b := NewModel(&captureDrawModel{keep: 31})
	node, _ := cfr.ProjectDrawNode(cfr.Draw1, cfr.BB, cfr.PolicyHistory{}, -1)
	duplicate := cards.MustParse("2c", "2c", "4h", "5s", "7d")
	if _, ok := b.ProjectedDraw(node, cfr.BB, duplicate, [2]cfr.DrawCounts{}, -1); ok {
		t.Fatal("accepted duplicate hand")
	}
	if _, ok := b.ProjectedDraw(node, cfr.Btn, cards.MustParse("2c", "3d", "4h", "5s", "7d"), [2]cfr.DrawCounts{}, -1); ok {
		t.Fatal("accepted wrong actor")
	}
}

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
