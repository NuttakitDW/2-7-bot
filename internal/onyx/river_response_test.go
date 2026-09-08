package onyx

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/table"
	"github.com/nuttakit/2-7-bot/internal/wire"
	"testing"
)

func TestRiverResponseEVPricesFoldCallAndRaise(t *testing.T) {
	for _, tc := range []struct {
		name               string
		response           int
		value              uint32
		wantCheck, wantBet float64
	}{
		{"better hand folds", 0, 300, 0, 5},
		{"better hand calls", 1, 300, 0, -1},
		{"worse hand calls", 1, 100, 5, 6},
		{"better hand raises", 2, 300, 0, -1},
		{"worse hand raises", 2, 100, 5, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r [5]handRange
			r[tc.response] = makeRange([]weightedValue{{tc.value, 20}})
			check, bet, ok := responseEV(deuce.Value(200), 5, 1, true, r, r[2].equity(200), 0)
			if !ok || check != tc.wantCheck || bet != tc.wantBet {
				t.Fatalf("check=%v bet=%v ok=%v", check, bet, ok)
			}
		})
	}
}

func TestResponseBetFollowsModeledCallAfterPatRaise(t *testing.T) {
	originalProfile, originalRanges := riverProfile, riverResponseRanges
	riverProfile = "response"
	t.Cleanup(func() { riverProfile, riverResponseRanges = originalProfile, originalRanges })
	// Our pat nine beats the observed pat-raise response distribution.
	v := uint32(deuce.Eval(cards.MustParse("2c", "3d", "4h", "7s", "Tc")))
	riverResponseRanges[1] = [5]handRange{}
	riverResponseRanges[1][2] = makeRange([]weightedValue{{v, 20}})
	b := setup("2c", "3d", "4h", "7s", "9c")
	b.Table.Hand.Street = table.Draw3
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 0, Count: 0})
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
	b.pot = 500
	bet := wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: &wire.Range{MinTo: 100, MaxTo: 100}}
	if a := b.Decide(bet); a.Kind != wire.ActionBet {
		t.Fatalf("did not take profitable bet: %+v", a)
	}
	b.Observe(wire.Event{Kind: wire.EventActed, Seat: 0, Action: wire.Bet(100), StreetCommit: 100})
	b.Observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Raise(200), StreetCommit: 200})
	if !b.strongPat {
		t.Fatal("test did not exercise strong-pat transition")
	}
	call := uint64(100)
	if a := b.Decide(wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call}); a.Kind != wire.ActionCall {
		t.Fatalf("abandoned modeled continuation: %+v", a)
	}
}

func TestOutOfPositionCheckAccountsForOpponentBet(t *testing.T) {
	var r [5]handRange
	r[0] = makeRange([]weightedValue{{300, 20}})
	r[4] = makeRange([]weightedValue{{100, 20}})
	check, bet, ok := responseEV(200, 5, 1, false, r, 0, 1)
	if !ok || check != 6 || bet != 5 {
		t.Fatalf("check=%v bet=%v ok=%v", check, bet, ok)
	}
	r[4] = handRange{}
	if _, _, ok := responseEV(200, 5, 1, false, r, 0, 1); ok {
		t.Fatal("accepted missing check-response evidence")
	}
}
