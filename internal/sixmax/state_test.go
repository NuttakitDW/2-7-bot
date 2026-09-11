package sixmax

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/sixmaxrange"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func newSixState(hero int) *State {
	s := NewState()
	cap := uint8(4)
	s.Hello(wire.Message{GameID: "27td-fl", SeatCount: 6, StartingStack: 10000,
		Stakes:  wire.Stakes{SmallBlind: 50, BigBlind: 100, Ante: 10},
		Betting: wire.Betting{Kind: "fixed-limit", RaiseCap: &cap}})
	s.HandStart(wire.Message{HandNo: 7, Seat: hero})
	s.Observe(wire.Event{Kind: wire.EventHandStart, Button: 0, Stacks: []uint64{10000, 10000, 10000, 10000, 10000, 10000}})
	return s
}

func TestSixPositionsRotateFromButton(t *testing.T) {
	s := newSixState(0)
	want := []Position{Button, SmallBlind, BigBlind, UnderTheGun, Hijack, Cutoff}
	for seat := range want {
		if got := s.Position(seat); got != want[seat] {
			t.Errorf("seat %d position = %v, want %v", seat, got, want[seat])
		}
	}
	s.Hand.Button = 4
	if got := s.Position(3); got != Cutoff {
		t.Fatalf("rotated seat 3 = %v, want cutoff", got)
	}
}

func TestPotUsesCommitmentDeltasAndAntes(t *testing.T) {
	s := newSixState(0)
	s.Observe(wire.Event{Kind: wire.EventPost, Seat: 0, PostKind: "ante", Amount: 10})
	s.Observe(wire.Event{Kind: wire.EventPost, Seat: 1, PostKind: "small-blind", Amount: 50})
	s.Observe(wire.Event{Kind: wire.EventPost, Seat: 2, PostKind: "big-blind", Amount: 100})
	s.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Predraw, Label: "predraw"})
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Call(), StreetCommit: 100})
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Call(), StreetCommit: 100})
	if s.Hand.Pot != 210 || s.Hand.Seats[1].Contribution != 100 {
		t.Fatalf("pot/contribution = %d/%d, want 210/100", s.Hand.Pot, s.Hand.Seats[1].Contribution)
	}
	s.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw1, Label: "draw1"})
	if s.Hand.Seats[1].StreetCommit != 0 || s.Hand.Pot != 210 {
		t.Fatalf("street reset changed persistent state: %+v", s.Hand.Seats[1])
	}
}

func TestShortAllInCreatesStickySidePot(t *testing.T) {
	s := newSixState(0)
	s.Observe(wire.Event{Kind: wire.EventPost, Seat: 1, PostKind: "small-blind", Amount: 50, AllIn: true})
	if s.Hand.SidePot {
		t.Fatal("equal current contribution prematurely marked side pot")
	}
	s.Observe(wire.Event{Kind: wire.EventPost, Seat: 2, PostKind: "big-blind", Amount: 100})
	if !s.Hand.SidePot || !s.Hand.Seats[1].AllIn {
		t.Fatalf("short all-in state=%+v", s.Hand)
	}
}

func TestPerOpponentDrawsExcludeFoldedAndPreserveStalePat(t *testing.T) {
	s := newSixState(0)
	s.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw1})
	s.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
	s.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 2, Count: 2, AllIn: true})
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Fold()})
	s.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw2})
	reads := s.LiveOpponentDraws()
	if len(reads) != 4 {
		t.Fatalf("live opponent reads = %d, want 4 after one fold", len(reads))
	}
	for _, read := range reads {
		if read.Seat == 1 {
			t.Fatal("folded stale pat remained in live reads")
		}
		if read.Seat == 2 && (!read.Known || read.Count != 2 || read.StreetsAgo != 1 || !s.Hand.Seats[2].AllIn) {
			t.Fatalf("all-in stale draw = %+v", read)
		}
	}
}

func TestHandResetClearsCardsFoldsAllInAndDraws(t *testing.T) {
	s := newSixState(0)
	s.Observe(wire.Event{Kind: wire.EventDealHole, Seat: 0, Cards: cards.MustParse("2c", "3d", "4h", "5s", "7c")})
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 3, Action: wire.Fold(), AllIn: true})
	s.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 4, Count: 0})
	s.HandStart(wire.Message{HandNo: 8, Seat: 5})
	if s.Hand.No != 8 || s.Hand.Hero != 5 || len(s.Hand.Cards) != 0 || s.Hand.Seats[3].Folded || s.Hand.Seats[3].AllIn {
		t.Fatalf("hand did not reset: %+v", s.Hand)
	}
	if _, ok := s.LatestDraw(4); ok {
		t.Fatal("old draw survived hand reset")
	}
}

func TestOurPreviouslyDiscardedCardMayReturn(t *testing.T) {
	s := newSixState(0)
	s.Hand.Cards = cards.MustParse("2c", "3d", "4h", "5s", "Kc")
	s.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw1})
	s.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 0,
		Discarded: cards.MustParse("Kc"), Drawn: cards.MustParse("Kd"), Count: 1})
	s.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw2})
	s.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 0,
		Discarded: cards.MustParse("Kd"), Drawn: cards.MustParse("Kc"), Count: 1})
	if got := cards.Strings(s.Hand.Cards); got[len(got)-1] != "Kc" {
		t.Fatalf("returned discard missing: %v", got)
	}
}

func TestNewWagerReopensActionForEarlierChecksAndReraises(t *testing.T) {
	s := newSixState(0)
	s.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw3})
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Check(), StreetCommit: 0})
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 2, Action: wire.Check(), StreetCommit: 0})
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 3, Action: wire.Bet(200), StreetCommit: 200})
	if s.Hand.Seats[1].ActedOnStreet || s.Hand.Seats[2].ActedOnStreet || !s.Hand.Seats[3].ActedOnStreet {
		t.Fatalf("bet did not reopen checked seats: %+v", s.Hand.Seats)
	}
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 4, Action: wire.Raise(400), StreetCommit: 400})
	if s.Hand.Seats[3].ActedOnStreet || !s.Hand.Seats[4].ActedOnStreet {
		t.Fatalf("reraise did not reopen bettor: %+v", s.Hand.Seats)
	}
}

func TestTracksOnlyHerosOwnPredrawRaises(t *testing.T) {
	s := newSixState(4)
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 3, Action: wire.Raise(200), StreetCommit: 200})
	if s.Hand.HeroPredrawRaises != 0 {
		t.Fatal("opponent raise counted as hero open")
	}
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 4, Action: wire.Raise(300), StreetCommit: 300})
	if s.Hand.HeroPredrawRaises != 1 {
		t.Fatalf("hero raises = %d, want 1", s.Hand.HeroPredrawRaises)
	}
	s.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw1})
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 4, Action: wire.Raise(200), StreetCommit: 200})
	if s.Hand.HeroPredrawRaises != 1 {
		t.Fatal("postdraw raise changed predraw history")
	}
}

func TestRuntimeRangeContextMatchesPublicPrefixBuilder(t *testing.T) {
	s := newSixState(0)
	s.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw2})
	s.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
	s.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw3})
	s.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 1})
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Bet(200), StreetCommit: 200})
	s.Observe(wire.Event{Kind: wire.EventActed, Seat: 2, Action: wire.Call(), StreetCommit: 200})
	var draws [6][3]int
	for seat := range draws {
		draws[seat] = [3]int{-1, -1, -1}
	}
	draws[1] = [3]int{-1, 0, 1}
	want, wantOK := sixmaxrange.BuildContext(1, draws, []sixmaxrange.PublicAction{{Seat: 1, Street: 3, Action: "bet"}, {Seat: 2, Street: 3, Action: "call"}}, 6)
	got, gotOK := s.RangeContext(1)
	if got != want || gotOK != wantOK {
		t.Fatalf("runtime=%+v/%t offline=%+v/%t", got, gotOK, want, wantOK)
	}
}
