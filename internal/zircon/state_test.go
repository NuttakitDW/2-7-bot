package zircon

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func testState(hero int) *State {
	s := NewState()
	cap := uint8(4)
	s.Hello(wire.Message{GameID: "27td-fl", SeatCount: 6, StartingStack: 10000,
		Stakes: wire.Stakes{SmallBlind: 50, BigBlind: 100}, Betting: wire.Betting{Kind: "fixed-limit", RaiseCap: &cap}})
	s.HandStart(wire.Message{HandNo: 1, Seat: hero})
	s.Observe(wire.Event{Kind: wire.EventHandStart, Button: 0, Stacks: []uint64{10000, 10000, 10000, 10000, 10000, 10000}})
	return s
}

func TestPositionsAndCommitmentDeltas(t *testing.T) {
	s := testState(3)
	want := []Position{Button, SmallBlind, BigBlind, UnderTheGun, Hijack, Cutoff}
	for seat := range want {
		if got := s.Position(seat); got != want[seat] {
			t.Fatalf("seat %d=%s", seat, got)
		}
	}
	s.Observe(wire.Event{Kind: wire.EventPost, Seat: 0, PostKind: "ante", Amount: 10})
	s.Observe(wire.Event{Kind: wire.EventPost, Seat: 1, PostKind: "small-blind", Amount: 50})
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Call(), StreetCommit: 100})
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Call(), StreetCommit: 100})
	if s.Hand.Pot != 110 || s.Hand.Seats[1].Contribution != 100 {
		t.Fatalf("pot/contribution=%d/%d", s.Hand.Pot, s.Hand.Seats[1].Contribution)
	}
}

func TestFoldedPatRemovedAndPendingReopens(t *testing.T) {
	s := testState(0)
	s.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw2})
	s.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Fold()})
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 2, Action: wire.Check()})
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 3, Action: wire.Bet(200), StreetCommit: 200})
	if s.Hand.Seats[2].ActedOnStreet {
		t.Fatal("new wager did not reopen checker")
	}
	for _, read := range s.LiveOpponentDraws() {
		if read.Seat == 1 {
			t.Fatal("folded pat remains live")
		}
	}
}

func TestDrawFreshnessAndReturnedDiscard(t *testing.T) {
	s := testState(0)
	s.Hand.Cards = cards.MustParse("2c", "3d", "4h", "5s", "Kc")
	s.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw1})
	s.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
	s.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 0, Count: 1, Discarded: cards.MustParse("Kc"), Drawn: cards.MustParse("Kd")})
	s.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw2})
	read, ok := s.LatestDraw(1)
	if !ok || read.Count != 0 || read.StreetsAgo != 1 {
		t.Fatalf("read=%+v", read)
	}
	s.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 0, Count: 1, Discarded: cards.MustParse("Kd"), Drawn: cards.MustParse("Kc")})
	if !cards.NewSet(s.Hand.Cards).Has(cards.MustParse("Kc")[0]) {
		t.Fatal("reshuffled discard not restored")
	}
}

func TestShortAllInCreatesSidePot(t *testing.T) {
	s := testState(0)
	s.Observe(wire.Event{Kind: wire.EventPost, Seat: 1, PostKind: "small-blind", Amount: 50, AllIn: true})
	s.Observe(wire.Event{Kind: wire.EventPost, Seat: 2, PostKind: "big-blind", Amount: 100})
	if !s.Hand.SidePot {
		t.Fatal("short all-in did not create side pot")
	}
}
