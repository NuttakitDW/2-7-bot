package lapis

import (
	"bytes"
	"encoding/json"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func blockerResponsePayload(t *testing.T) []byte {
	t.Helper()
	base := cfr.Empirical{Version: 1}
	for i := range base.Betting {
		base.Betting[i] = []cfr.PolicyNode{{Feature: -1, Prob: [6]float64{0, 1}}}
	}
	for i := range base.Drawing {
		base.Drawing[i] = []cfr.PolicyNode{{Feature: -1, Prob: [6]float64{1}}}
	}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var payload bytes.Buffer
	if err := cfr.EncodeRiverResponse(raw, &cfr.RiverPolicy{Version: 1, Abstraction: "rank"}, &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Bytes()
}

func TestBlockerTrackerUsesOnlyHeroPrivateCardsAndResets(t *testing.T) {
	oldData, oldParticles := blueprintData, BlockerParticles
	blueprintData, BlockerParticles = blockerResponsePayload(t), "64"
	t.Cleanup(func() { blueprintData, BlockerParticles = oldData, oldParticles })
	b, err := NewBlockerRiverResponse()
	if err != nil {
		t.Fatal(err)
	}
	b.Hello(wire.Message{GameID: "27td-fl", SeatCount: 2})
	b.HandStart(wire.Message{Seat: cfr.Btn})
	b.Observe(wire.Event{Kind: wire.EventHandStart, Button: cfr.Btn})
	hero := cards.MustParse("7c", "5d", "4h", "3s", "2c")
	b.Observe(wire.Event{Kind: wire.EventDealHole, Seat: cfr.Btn, Cards: hero})
	privateOpponent := cards.MustParse("7d", "6d", "5h", "4s", "3c")
	b.Observe(wire.Event{Kind: wire.EventDealHole, Seat: cfr.BB, Cards: privateOpponent})
	b.Observe(acted(cfr.Btn, wire.ActionCall))
	b.Observe(acted(cfr.BB, wire.ActionCheck))
	b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: cfr.Draw1})
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: cfr.BB, Count: 1, Discarded: privateOpponent[:1], Drawn: privateOpponent[1:2]})
	discarded := cards.MustParse("7c")
	drawn := cards.MustParse("8c")
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: cfr.Btn, Count: 1, Discarded: discarded, Drawn: drawn})
	wantKnown := cards.NewSet(hero) | cards.NewSet(discarded) | cards.NewSet(drawn)
	if b.blockers.known != wantKnown {
		t.Fatalf("known = %v, want hero-only %v", b.blockers.known, wantKnown)
	}
	if len(b.blockers.history) != 2 || b.blockers.history[0].Draw || !b.blockers.history[1].Draw {
		t.Fatalf("opponent public history = %+v", b.blockers.history)
	}
	if b.blockers.history[1].View.Node == b.node || b.blockers.history[1].View.Drawn[cfr.BB][cfr.Draw1] != -1 {
		t.Fatal("opponent draw was not captured from the pre-mutation public node")
	}
	b.HandStart(wire.Message{Seat: cfr.Btn})
	if b.blockers.known != 0 || len(b.blockers.history) != 0 || !b.blockers.complete {
		t.Fatalf("blocker state did not reset: %+v", b.blockers)
	}
}

func TestBlockerRiverResponsePreservesBaseBeforeRiver(t *testing.T) {
	oldData, oldParticles := blueprintData, BlockerParticles
	blueprintData, BlockerParticles = blockerResponsePayload(t), "64"
	t.Cleanup(func() { blueprintData, BlockerParticles = oldData, oldParticles })
	base, err := NewRiverResponse()
	if err != nil {
		t.Fatal(err)
	}
	spinel, err := NewBlockerRiverResponse()
	if err != nil {
		t.Fatal(err)
	}
	base.rng = rand.New(rand.NewPCG(11, 29))
	spinel.rng = rand.New(rand.NewPCG(11, 29))
	for _, b := range []*Bot{base, spinel} {
		b.Hello(wire.Message{GameID: "27td-fl", SeatCount: 2})
		b.HandStart(wire.Message{Seat: cfr.Btn})
		b.Observe(wire.Event{Kind: wire.EventHandStart, Button: cfr.Btn})
		b.Observe(wire.Event{Kind: wire.EventDealHole, Seat: cfr.Btn, Cards: cards.MustParse("7c", "5d", "4h", "3s", "2c")})
	}
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: new(uint64), Raise: &wire.Range{MinTo: 200, MaxTo: 200}}
	if got, want := spinel.Decide(d), base.Decide(d); !reflect.DeepEqual(got, want) {
		t.Fatalf("predraw Spinel = %+v, Tourmaline = %+v", got, want)
	}
}

func TestBlockerRiverResponseConfiguration(t *testing.T) {
	oldData, oldParticles := blueprintData, BlockerParticles
	blueprintData = blockerResponsePayload(t)
	t.Cleanup(func() { blueprintData, BlockerParticles = oldData, oldParticles })
	for _, value := range []string{"nope", "63", "8193"} {
		BlockerParticles = value
		if _, err := NewBlockerRiverResponse(); err == nil {
			t.Fatalf("accepted particle count %q", value)
		}
	}
}

func TestBlockerRiverResponseReachesFinalStreetOverride(t *testing.T) {
	oldData, oldParticles := blueprintData, BlockerParticles
	blueprintData, BlockerParticles = blockerResponsePayload(t), "64"
	t.Cleanup(func() { blueprintData, BlockerParticles = oldData, oldParticles })
	b, err := NewBlockerRiverResponse()
	if err != nil {
		t.Fatal(err)
	}
	b.Hello(wire.Message{GameID: "27td-fl", SeatCount: 2})
	b.HandStart(wire.Message{Seat: cfr.BB})
	b.Observe(wire.Event{Kind: wire.EventHandStart, Button: cfr.Btn})
	b.Observe(wire.Event{Kind: wire.EventDealHole, Seat: cfr.BB, Cards: cards.MustParse("7c", "5d", "4h", "3s", "2c")})
	b.Observe(acted(cfr.Btn, wire.ActionCall))
	b.Observe(acted(cfr.BB, wire.ActionCheck))
	for street := cfr.Draw1; street <= cfr.Draw3; street++ {
		b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: street})
		b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: cfr.BB, Count: 0})
		b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: cfr.Btn, Count: 0})
		if street != cfr.Draw3 {
			b.Observe(acted(cfr.BB, wire.ActionCheck))
			b.Observe(acted(cfr.Btn, wire.ActionCheck))
		}
	}
	d := wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: &wire.Range{MinTo: 200, MaxTo: 200}}
	if action := b.Decide(d); action.Kind != wire.ActionBet || b.blockers.solves != 1 {
		t.Fatalf("blocker override = %+v, solves %d", action, b.blockers.solves)
	}
}

func TestBlockerRiverResponseFallsBackAfterIncompletePrivateEvent(t *testing.T) {
	oldData, oldParticles := blueprintData, BlockerParticles
	blueprintData, BlockerParticles = blockerResponsePayload(t), "64"
	t.Cleanup(func() { blueprintData, BlockerParticles = oldData, oldParticles })
	b, err := NewBlockerRiverResponse()
	if err != nil {
		t.Fatal(err)
	}
	b.Hello(wire.Message{GameID: "27td-fl", SeatCount: 2})
	b.HandStart(wire.Message{Seat: cfr.BB})
	b.Observe(wire.Event{Kind: wire.EventHandStart, Button: cfr.Btn})
	b.Observe(wire.Event{Kind: wire.EventDealHole, Seat: cfr.BB, Cards: cards.MustParse("7c", "5d", "4h", "3s", "2c")})
	b.Observe(acted(cfr.Btn, wire.ActionCall))
	b.Observe(acted(cfr.BB, wire.ActionCheck))
	b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: cfr.Draw1})
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: cfr.BB, Count: 1})
	if b.blockers.complete {
		t.Fatal("accepted missing hero discard and draw cards")
	}
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: cfr.Btn, Count: 0})
	for street := cfr.Draw1; street < cfr.Draw3; street++ {
		b.Observe(acted(cfr.BB, wire.ActionCheck))
		b.Observe(acted(cfr.Btn, wire.ActionCheck))
		b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: street + 1})
		b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: cfr.BB, Count: 0})
		b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: cfr.Btn, Count: 0})
	}
	d := wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: &wire.Range{MinTo: 200, MaxTo: 200}}
	if action := b.Decide(d); action.Kind != wire.ActionCheck || b.blockers.solves != 0 {
		t.Fatalf("incomplete tracker did not use base fallback: %+v, solves %d", action, b.blockers.solves)
	}
}

func TestBlockerRiverResponseRejectsImpossiblePrivateDraw(t *testing.T) {
	oldData, oldParticles := blueprintData, BlockerParticles
	blueprintData, BlockerParticles = blockerResponsePayload(t), "64"
	t.Cleanup(func() { blueprintData, BlockerParticles = oldData, oldParticles })
	tests := []wire.Event{
		{Kind: wire.EventDrawResult, Seat: cfr.BB, Count: 1, Discarded: cards.MustParse("As"), Drawn: cards.MustParse("8c")},
		{Kind: wire.EventDrawResult, Seat: cfr.BB, Count: 1, Discarded: cards.MustParse("7c"), Drawn: cards.MustParse("7c")},
	}
	for _, event := range tests {
		b, err := NewBlockerRiverResponse()
		if err != nil {
			t.Fatal(err)
		}
		b.Hello(wire.Message{GameID: "27td-fl", SeatCount: 2})
		b.HandStart(wire.Message{Seat: cfr.BB})
		b.Observe(wire.Event{Kind: wire.EventHandStart, Button: cfr.Btn})
		b.Observe(wire.Event{Kind: wire.EventDealHole, Seat: cfr.BB, Cards: cards.MustParse("7c", "5d", "4h", "3s", "2c")})
		b.Observe(acted(cfr.Btn, wire.ActionCall))
		b.Observe(acted(cfr.BB, wire.ActionCheck))
		b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: cfr.Draw1})
		b.Observe(event)
		if b.blockers.complete {
			t.Fatalf("accepted impossible private event %+v", event)
		}
	}
}

func TestSameRiverHandWithDifferentMuckChangesBeliefAndEV(t *testing.T) {
	oldData, oldParticles := blueprintData, BlockerParticles
	blueprintData, BlockerParticles = blockerResponsePayload(t), "64"
	t.Cleanup(func() { blueprintData, BlockerParticles = oldData, oldParticles })
	b, err := NewBlockerRiverResponse()
	if err != nil {
		t.Fatal(err)
	}
	heroCards := cards.MustParse("9c", "7d", "5h", "3s", "2c")
	known := cards.NewSet(heroCards)
	muck := cards.NewSet(cards.MustParse("2d"))
	withoutMuck := cfr.OpponentBelief(b.blockers.model, nil, known, rand.New(rand.NewPCG(31, 47)), 8192)
	withMuck := cfr.OpponentBelief(b.blockers.model, nil, known|muck, rand.New(rand.NewPCG(31, 47)), 8192)
	foundBefore := false
	for _, particle := range withoutMuck {
		foundBefore = foundBefore || cards.NewSet(particle.Hand[:])&muck != 0
	}
	for _, particle := range withMuck {
		if cards.NewSet(particle.Hand[:])&muck != 0 {
			t.Fatal("mucked card remained in blocker-conditioned belief")
		}
	}
	if !foundBefore {
		t.Fatal("unblocked belief fixture never sampled the mucked card")
	}
	tree := &cfr.Tree{Nodes: []cfr.Node{
		{Kind: cfr.KindBet, Street: cfr.Draw3, Actor: cfr.BB, Facing: true, Acts: []uint8{cfr.Fold, cfr.Pass}, Next: [3]int32{1, 2}, Commit: [2]int32{200, 100}},
		{Kind: cfr.KindFold, Street: cfr.Draw3, Actor: cfr.BB, Commit: [2]int32{200, 100}},
		{Kind: cfr.KindShowdown, Street: cfr.Draw3, Commit: [2]int32{200, 200}},
	}}
	view := cfr.View{Seat: cfr.BB, Street: cfr.Draw3, Node: 0, Facing: true}
	copy(view.Hand[:], cards.SortedByRank(heroCards))
	_, evWithout, okWithout := cfr.BestRiverAction(tree, 0, view, withoutMuck, b.blockers.model)
	_, evWith, okWith := cfr.BestRiverAction(tree, 0, view, withMuck, b.blockers.model)
	if !okWithout || !okWith || math.Abs(evWithout-evWith) < 0.01 {
		t.Fatalf("muck did not change river EV: without %.4f (%v), with %.4f (%v)", evWithout, okWithout, evWith, okWith)
	}
}

func TestEmbeddedRiverResponseChangesTheWireAction(t *testing.T) {
	old := blueprintData
	t.Cleanup(func() { blueprintData = old })
	base := cfr.Empirical{Version: 1}
	for i := range base.Betting {
		base.Betting[i] = []cfr.PolicyNode{{Feature: -1, Prob: [6]float64{0, 1}}}
	}
	for i := range base.Drawing {
		base.Drawing[i] = []cfr.PolicyNode{{Feature: -1, Prob: [6]float64{1}}}
	}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	// Independent encoding oracle: 200-chip starting pot, no previous
	// raises, root river path 1, legacy nuts bucket 0, BB, all pat, no aggressor.
	context := uint64(2<<12)<<9 | 1
	key := (((context<<13)<<1 | cfr.BB) << 8) << 2
	policy := &cfr.RiverPolicy{Version: 1, Abstraction: "legacy", Rows: []cfr.RiverRow{{Key: key, P: [3]uint8{0, 0, 255}, Visits: 1000000}}}
	var payload bytes.Buffer
	if err := cfr.EncodeRiverResponse(raw, policy, &payload); err != nil {
		t.Fatal(err)
	}
	blueprintData = payload.Bytes()
	b, err := NewRiverResponse()
	if err != nil {
		t.Fatal(err)
	}
	b.Hello(wire.Message{GameID: "27td-fl", SeatCount: 2})
	b.HandStart(wire.Message{Seat: cfr.BB})
	b.Observe(wire.Event{Kind: wire.EventHandStart, Button: cfr.Btn})
	b.Observe(wire.Event{Kind: wire.EventDealHole, Seat: cfr.BB, Cards: cards.MustParse("7c", "5d", "4h", "3s", "2c")})
	b.Observe(acted(cfr.Btn, wire.ActionCall))
	b.Observe(acted(cfr.BB, wire.ActionCheck))
	for street := cfr.Draw1; street <= cfr.Draw3; street++ {
		b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: street})
		b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: cfr.BB, Count: 0})
		b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: cfr.Btn, Count: 0})
		if street != cfr.Draw3 {
			b.Observe(acted(cfr.BB, wire.ActionCheck))
			b.Observe(acted(cfr.Btn, wire.ActionCheck))
		}
	}
	d := wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: &wire.Range{MinTo: 200, MaxTo: 200}}
	if action := b.Decide(d); action.Kind != wire.ActionBet || action.To != 200 || b.Fallbacks != 0 {
		t.Fatalf("embedded override did not replace base check: %+v, fallbacks %d", action, b.Fallbacks)
	}
	// Removing the trained row restores the frozen base policy through the
	// same tracker and the same decision, proving the row caused the bet.
	policy.Rows = nil
	payload.Reset()
	if err := cfr.EncodeRiverResponse(raw, policy, &payload); err != nil {
		t.Fatal(err)
	}
	m, err := cfr.DecodeRiverResponse(payload.Bytes(), b.tree)
	if err != nil {
		t.Fatal(err)
	}
	b.player = m
	if action := b.Decide(d); action.Kind != wire.ActionCheck {
		t.Fatalf("base should check: %+v", action)
	}
}
