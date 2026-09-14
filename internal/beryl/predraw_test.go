package beryl

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestOpeningChartByPositionAndShape(t *testing.T) {
	tests := []struct {
		name string
		pos  Position
		hand []cards.Card
		want bool
	}{
		{"utg permits 2345 because ace is high", UnderTheGun, cards.MustParse("2c", "3d", "4h", "5s", "Kc"), true},
		{"utg rejects true open ended 3456", UnderTheGun, cards.MustParse("3c", "4d", "5h", "6s", "Kc"), false},
		{"hijack seven under need not contain deuce", Hijack, cards.MustParse("3c", "4d", "5h", "7s", "Kc"), true},
		{"cutoff nonconsecutive three card eight", Cutoff, cards.MustParse("3c", "5d", "8h", "Ks", "Kc"), true},
		{"button two seven", Button, cards.MustParse("2c", "7d", "Jh", "Qs", "Kc"), true},
		{"utg cannot enter on two seven", UnderTheGun, cards.MustParse("2c", "7d", "Jh", "Qs", "Kc"), false},
		{"hijack two six king is outside two-x-six", Hijack, cards.MustParse("2c", "6d", "Jh", "Qs", "Kc"), false},
		{"button may enter two six as three-card draw", Button, cards.MustParse("2c", "6d", "Jh", "Qs", "Kc"), true},
		{"flush is not accepted as a pat", UnderTheGun, cards.MustParse("3c", "4c", "5c", "6c", "9c"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := openingPlayable(tt.hand, tt.pos); got != tt.want {
				t.Fatalf("openingPlayable(%v, %s) = %v, want %v", tt.hand, tt.pos, got, tt.want)
			}
		})
	}
}

func TestFacingSingleOpenUsesOpenerPosition(t *testing.T) {
	hand := cards.MustParse("2c", "4d", "8h", "Qs", "Qc")
	if got := facingOpenAction(hand, Button, UnderTheGun); got != rangeFold {
		t.Fatalf("vs UTG = %v, want fold", got)
	}
	if got := facingOpenAction(hand, Button, Cutoff); got != rangeCall {
		t.Fatalf("vs CO = %v, want call", got)
	}
	if got := facingOpenAction(cards.MustParse("2c", "3d", "5h", "8s", "Kc"), Button, UnderTheGun); got != rangeRaise {
		t.Fatalf("four-card 8 no-SD = %v, want raise", got)
	}
	if got := facingOpenAction(cards.MustParse("2c", "7d", "Jh", "Qs", "Kc"), BigBlind, Button); got != rangeCall {
		t.Fatalf("BB 2-7 vs BTN = %v, want call", got)
	}
	if got := facingOpenAction(cards.MustParse("2c", "7d", "Jh", "Qs", "Kc"), SmallBlind, Button); got != rangeFold {
		t.Fatalf("SB cold call vs BTN = %v, want fold", got)
	}
}

func TestTrackerRotatesPositionsAndPreservesRaisedLimpedCappedHistory(t *testing.T) {
	s := newState()
	s.hello(wire.Message{SeatCount: 6})
	s.handStart(wire.Message{Seat: 5})
	s.observe(wire.Event{Kind: wire.EventHandStart, Button: 2})
	if got := s.position(5); got != UnderTheGun {
		t.Fatalf("position = %s", got)
	}
	s.observe(wire.Event{Kind: wire.EventActed, Seat: 5, Action: wire.Call()})
	s.observe(wire.Event{Kind: wire.EventActed, Seat: 0, Action: wire.Raise(200), StreetCommit: 200})
	s.observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Raise(300), StreetCommit: 300})
	s.observe(wire.Event{Kind: wire.EventActed, Seat: 5, Action: wire.Call(), StreetCommit: 300})
	if s.firstOpener != 0 || s.predrawRaises != 2 || s.predrawCalls != 2 || !s.heroVoluntary {
		t.Fatalf("tracker = opener %d raises %d calls %d voluntary %v", s.firstOpener, s.predrawRaises, s.predrawCalls, s.heroVoluntary)
	}
}
