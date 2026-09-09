package lapis

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

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
