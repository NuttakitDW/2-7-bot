package table

import (
	"strings"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// One complete hand, captured on 2026-08-22 from
//
//	poker-arena run --game 27td-fl --hands 200 --seed 7 \
//	  --bot a@builtin:random --bot b@builtin:random --log …
//
// and then redacted to what seat 0 would actually receive: the opponent's
// hole cards and draw cards are blanked, their draw *counts* are not. It runs
// all three draws and reaches a showdown, so tracking it correctly means the
// hand travelling through the decoder ends up as the hand the engine showed.
//
// Note the ordering on the first line of the street list: `street-start` for
// predraw arrives *after* the blinds are posted, while on draw streets it
// arrives before the draws.
const handEightAsSeat0 = `
{"event":"hand-start","hand_no":8,"button":0,"stacks":[10000,10000]}
{"event":"post","seat":0,"kind":"small-blind","amount":50,"all_in":false}
{"event":"post","seat":1,"kind":"big-blind","amount":100,"all_in":false}
{"event":"street-start","street":0,"label":"predraw"}
{"event":"deal-hole","seat":1,"cards":[],"count":5}
{"event":"deal-hole","seat":0,"cards":["3d","9c","6s","4c","Tc"],"count":5}
{"event":"acted","seat":0,"action":{"kind":"call"},"street_commit":100,"all_in":false}
{"event":"acted","seat":1,"action":{"kind":"raise","to":200},"street_commit":200,"all_in":false}
{"event":"acted","seat":0,"action":{"kind":"call"},"street_commit":200,"all_in":false}
{"event":"street-start","street":1,"label":"draw1"}
{"event":"draw-result","seat":1,"discarded":[],"drawn":[],"count":1}
{"event":"draw-result","seat":0,"discarded":["3d","Tc","6s","4c"],"drawn":["As","5h","2c","3c"],"count":4}
{"event":"acted","seat":1,"action":{"kind":"bet","to":100},"street_commit":100,"all_in":false}
{"event":"acted","seat":0,"action":{"kind":"raise","to":200},"street_commit":200,"all_in":false}
{"event":"acted","seat":1,"action":{"kind":"call"},"street_commit":200,"all_in":false}
{"event":"street-start","street":2,"label":"draw2"}
{"event":"draw-result","seat":1,"discarded":[],"drawn":[],"count":1}
{"event":"draw-result","seat":0,"discarded":["3c","As"],"drawn":["Kd","4s"],"count":2}
{"event":"acted","seat":1,"action":{"kind":"check"},"street_commit":0,"all_in":false}
{"event":"acted","seat":0,"action":{"kind":"check"},"street_commit":0,"all_in":false}
{"event":"street-start","street":3,"label":"draw3"}
{"event":"draw-result","seat":1,"discarded":[],"drawn":[],"count":3}
{"event":"draw-result","seat":0,"discarded":["Kd","5h","9c"],"drawn":["Qd","Jc","7s"],"count":3}
{"event":"acted","seat":1,"action":{"kind":"check"},"street_commit":0,"all_in":false}
{"event":"acted","seat":0,"action":{"kind":"check"},"street_commit":0,"all_in":false}
{"event":"showdown-show","seat":1,"cards":["7c","4h","Kc","Td","8d"],"hi":16021933,"lo":null}
{"event":"showdown-show","seat":0,"cards":["2c","4s","Qd","Jc","7s"],"hi":16083679,"lo":null}
{"event":"pot-awarded","pot":0,"side":"whole","winners":[[0,800]]}
{"event":"hand-end","nets":[400,-400]}
`

// replay feeds the fixture in, stopping just before the nth draw-result so a
// test can inspect mid-hand state. A negative stop replays everything.
func replay(t *testing.T, seat int, stopBeforeDraw int) *Table {
	t.Helper()
	table := New()
	table.Hello(wire.Message{
		Type: wire.MsgHello, GameID: "27td-fl", SeatCount: 2, StartingStack: 10000,
		Stakes: wire.Stakes{Kind: "blinds", SmallBlind: 50, BigBlind: 100},
	})
	table.HandStart(wire.Message{Type: wire.MsgHandStart, HandNo: 8, Seat: seat})

	draws := 0
	for _, line := range strings.Split(strings.TrimSpace(handEightAsSeat0), "\n") {
		var event wire.Event
		if err := event.UnmarshalJSON([]byte(line)); err != nil {
			t.Fatalf("decode %s: %v", line, err)
		}
		if event.Kind == wire.EventDrawResult {
			if stopBeforeDraw >= 0 && draws == stopBeforeDraw {
				return table
			}
			draws++
		}
		table.Observe(event)
	}
	return table
}

// The hand must travel through three draws and arrive as the hand the engine
// showed down — the strongest single check that the decoder is right.
func TestHoleCardsTrackThroughEveryDraw(t *testing.T) {
	table := replay(t, 0, -1)
	got := strings.Join(cards.Strings(cards.SortedByRank(table.Hand.Cards)), " ")
	if want := "2c 4s 7s Jc Qd"; got != want {
		t.Fatalf("final hand = %q, want %q", got, want)
	}
	if !table.Hand.Complete() {
		t.Error("Complete() = false on a five-card hand")
	}
	// The engine's own value for those cards, from the showdown line.
	if value := deuce.Eval(table.Hand.Cards); uint64(value) != 16083679 {
		t.Errorf("Eval = %d, engine says 16083679", value)
	}
	if got, want := table.Hand.Category(), deuce.Weak; got != want {
		t.Errorf("Category = %v, want %v", got, want)
	}
}

func TestMatchAndPositionAreRecorded(t *testing.T) {
	table := replay(t, 0, -1)
	if table.Match.GameID != "27td-fl" || table.Match.BigBlind != 100 {
		t.Errorf("match = %+v", table.Match)
	}
	if !table.Hand.OnButton() {
		t.Error("seat 0 should be on the button")
	}
	if replay(t, 1, -1).Hand.OnButton() {
		t.Error("seat 1 should not be on the button")
	}
	if got, want := table.Hand.Street, Draw3; got != want {
		t.Errorf("street = %d, want %d", got, want)
	}
}

func TestOpponentDrawCountsAreRecordedPerStreet(t *testing.T) {
	hand := replay(t, 0, -1).Hand
	tests := []struct {
		name   string
		seat   int
		street int
		want   int
	}{
		{"opponent drew one on draw1", 1, Draw1, 1},
		{"we drew four on draw1", 0, Draw1, 4},
		{"opponent drew one on draw2", 1, Draw2, 1},
		{"we drew two on draw2", 0, Draw2, 2},
		{"opponent drew three on draw3", 1, Draw3, 3},
		{"we drew three on draw3", 0, Draw3, 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, known := hand.DrawCount(test.seat, test.street)
			if !known || got != test.want {
				t.Errorf("DrawCount(%d, %d) = %d, %v; want %d, true",
					test.seat, test.street, got, known, test.want)
			}
		})
	}
	if _, known := hand.DrawCount(1, Predraw); known {
		t.Error("nobody draws on predraw, so no count should be known")
	}
}

// The positional asymmetry that the draw rule turns on: everyone draws in
// seat order from left of the button, so heads-up the big blind decides its
// discard before the button has drawn, and the button decides knowing it.
func TestDrawOrderGivesTheButtonAFresherRead(t *testing.T) {
	t.Run("big blind draws first and reads a stale count", func(t *testing.T) {
		// Stop before the third draw-result: draw1 and draw2's big-blind
		// draws have happened, the button's draw2 has not.
		hand := replay(t, 1, 2).Hand
		count, streetsAgo, known := hand.OpponentDraw(Draw2)
		if !known {
			t.Fatal("no opponent read at all")
		}
		if streetsAgo != 1 {
			t.Errorf("streetsAgo = %d, want 1 — the button has not drawn yet", streetsAgo)
		}
		if count != 4 {
			t.Errorf("count = %d, want 4 (the button's draw1)", count)
		}
	})

	t.Run("button draws last and reads the current street", func(t *testing.T) {
		// Stop before the fourth draw-result: the big blind has already
		// drawn on draw2, and the button is about to.
		hand := replay(t, 0, 3).Hand
		count, streetsAgo, known := hand.OpponentDraw(Draw2)
		if !known || streetsAgo != 0 || count != 1 {
			t.Errorf("OpponentDraw(Draw2) = %d, %d, %v; want 1, 0, true",
				count, streetsAgo, known)
		}
	})

	t.Run("nobody has drawn before draw1", func(t *testing.T) {
		hand := replay(t, 0, 0).Hand
		if _, _, known := hand.OpponentDraw(Draw1); known {
			t.Error("an opponent read exists before any draw")
		}
	})
}
