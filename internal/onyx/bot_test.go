package onyx

import (
	"math/rand/v2"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/table"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func setup(hand ...string) *Bot {
	b, _ := New()
	b.Hello(wire.Message{SeatCount: 2, Stakes: wire.Stakes{SmallBlind: 25, BigBlind: 50}})
	b.HandStart(wire.Message{Seat: 0})
	b.Observe(wire.Event{Kind: wire.EventDealHole, Seat: 0, Cards: cards.MustParse(hand...)})
	return b
}

func TestNewSeededHasReproduciblePrivateMix(t *testing.T) {
	a, err := NewSeeded(42)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSeeded(42)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewSeeded(43)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		av, bv, cv := a.random(), b.random(), c.random()
		if av != bv {
			t.Fatalf("same seed diverged at %d: %v != %v", i, av, bv)
		}
		if i == 0 && av == cv {
			t.Fatal("distinct seeds shared their first mix")
		}
	}
}

func TestLegalDecisionsAcrossStreetsAndHands(t *testing.T) {
	rng := rand.New(rand.NewPCG(9123, 44))
	b, _ := New()
	b.Hello(wire.Message{SeatCount: 2, Stakes: wire.Stakes{SmallBlind: 25, BigBlind: 50}})
	for n := 0; n < 300; n++ {
		deck := rng.Perm(52)
		hand := make([]cards.Card, 5)
		for i := range hand {
			hand[i] = cards.CardFromIndex(deck[i])
		}
		for street := 0; street <= 3; street++ {
			b.HandStart(wire.Message{Seat: n % 2})
			b.Table.Hand.Cards = hand
			b.Table.Hand.Street = street
			b.Table.Hand.Wagers = n % 4
			b.pot = uint64(100 + n%1200)
			b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1 - n%2, Count: n % 4})
			for _, facing := range []bool{false, true} {
				d := wire.Decision{Kind: wire.DecisionWager, Check: !facing, Fold: facing}
				if facing {
					call := uint64(100)
					d.Call = &call
					if n%4 != 3 {
						d.Raise = &wire.Range{MinTo: 200, MaxTo: 200}
					}
				} else {
					d.Bet = &wire.Range{MinTo: 100, MaxTo: 100}
				}
				a := b.Decide(d)
				switch a.Kind {
				case wire.ActionFold:
					if !d.Fold {
						t.Fatal("fold for free")
					}
				case wire.ActionCheck:
					if !d.Check {
						t.Fatal("check facing a bet")
					}
				case wire.ActionCall:
					if d.Call == nil {
						t.Fatal("call with no bet")
					}
				case wire.ActionBet:
					if d.Bet == nil || a.To != d.Bet.MinTo {
						t.Fatal("illegal bet")
					}
				case wire.ActionRaise:
					if d.Raise == nil || a.To != d.Raise.MinTo {
						t.Fatal("illegal raise")
					}
				default:
					t.Fatalf("unknown wager %+v", a)
				}
			}
			if street > 0 {
				a := b.Decide(wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5})
				if a.Kind != wire.ActionDiscard || cards.NewSet(a.Cards).Len() != len(a.Cards) {
					t.Fatalf("invalid draw %+v", a)
				}
				for _, c := range a.Cards {
					if !cards.NewSet(hand).Has(c) {
						t.Fatal("discard outside hand")
					}
				}
			}
		}
	}
}

func TestPotTracksContributionsNotRaiseTargets(t *testing.T) {
	b := setup("2c", "3d", "4h", "7s", "Qc")
	b.Observe(wire.Event{Kind: wire.EventPost, Seat: 0, Amount: 25})
	b.Observe(wire.Event{Kind: wire.EventPost, Seat: 1, Amount: 50})
	b.Observe(wire.Event{Kind: wire.EventActed, Seat: 0, Action: wire.Raise(100), StreetCommit: 100})
	b.Observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Call(), StreetCommit: 100})
	if b.pot != 200 {
		t.Fatalf("pot = %d, want 200", b.pot)
	}
	b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: 1})
	b.Observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Raise(50), StreetCommit: 50})
	if b.pot != 250 {
		t.Fatalf("pot after new street = %d", b.pot)
	}
	b.HandStart(wire.Message{Seat: 1})
	if b.pot != 0 || b.committed != [table.MaxSeats]uint64{} {
		t.Fatal("hand state leaked")
	}
}

func TestPreserveNineAgainstDrawingOpponent(t *testing.T) {
	b := setup("2c", "3d", "4h", "7s", "9c")
	b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: table.Draw3})
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 1})
	a := b.Decide(wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5})
	if len(a.Cards) != 0 {
		t.Fatalf("broke a made nine: %+v", a)
	}
}

func TestRiverJackCanCatchMissedDraw(t *testing.T) {
	b := setup("2c", "3d", "4h", "7s", "Jc")
	b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: table.Draw3})
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 1})
	b.Observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Raise(100), StreetCommit: 100})
	b.pot = 800
	call := uint64(100)
	a := b.Decide(wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call})
	if a.Kind != wire.ActionCall {
		t.Fatalf("folded bluff catcher at 8:1: %+v", a)
	}
}

func TestNutHandRaisesAndIncompleteHandIsLegal(t *testing.T) {
	b := setup("2c", "3d", "4h", "5s", "7c")
	b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: table.Draw3})
	d := wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: &wire.Range{MinTo: 100, MaxTo: 100}}
	if a := b.Decide(d); a.Kind != wire.ActionBet || a.To != 100 {
		t.Fatalf("nuts: %+v", a)
	}
	b.Table.Hand.Cards = nil
	if a := b.Decide(d); a.Kind != wire.ActionCheck {
		t.Fatalf("incomplete: %+v", a)
	}
}

func TestDoNotDefendWorthlessBigBlind(t *testing.T) {
	original := predrawDefense
	predrawDefense = "original"
	t.Cleanup(func() { predrawDefense = original })
	b := setup("6c", "9d", "Th", "Ks", "Ac")
	b.Table.Hand.Seat = 1
	b.Table.Hand.Wagers = 2
	call := uint64(50)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call}
	if a := b.Decide(d); a.Kind != wire.ActionFold {
		t.Fatalf("defended a poor three-card draw: %+v", a)
	}
}

func TestValueBetTenAfterOpponentMissesAndChecks(t *testing.T) {
	b := setup("2c", "3d", "4h", "7s", "Tc")
	b.Table.Hand.Street = table.Draw3
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 1})
	d := wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: &wire.Range{MinTo: 100, MaxTo: 100}}
	if a := b.Decide(d); a.Kind != wire.ActionBet {
		t.Fatalf("missed thin value: %+v", a)
	}
}

func TestFoldTenAgainstPatRiverBet(t *testing.T) {
	b := setup("2c", "3d", "4h", "7s", "Tc")
	b.Table.Hand.Street = table.Draw3
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
	b.Table.Hand.Wagers = 1
	b.pot = 400
	call := uint64(100)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call}
	if a := b.Decide(d); a.Kind != wire.ActionFold {
		t.Fatalf("paid a pat value range: %+v", a)
	}
}

func TestDoNotCapEightAgainstPatRaise(t *testing.T) {
	b := setup("2c", "3d", "4h", "6s", "8c")
	b.Table.Hand.Street = table.Draw3
	b.Table.Hand.Wagers = 2
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
	b.pot = 800
	call := uint64(100)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call, Raise: &wire.Range{MinTo: 300, MaxTo: 300}}
	if a := b.Decide(d); a.Kind != wire.ActionCall {
		t.Fatalf("overplayed eight against a raise: %+v", a)
	}
}

func TestDoNotRaiseRoughEightAgainstPatBet(t *testing.T) {
	b := setup("2c", "3d", "6h", "7s", "8c")
	b.Table.Hand.Street = table.Draw3
	b.Table.Hand.Wagers = 1
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
	b.pot = 500
	call := uint64(100)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call, Raise: &wire.Range{MinTo: 200, MaxTo: 200}}
	if a := b.Decide(d); a.Kind != wire.ActionCall {
		t.Fatalf("raised marginal eight: %+v", a)
	}
}

func TestDefendThreeCardDrawWithTwoUsefulLowCards(t *testing.T) {
	b := setup("4c", "5d", "Th", "Ks", "Ac")
	b.Table.Hand.Seat = 1
	b.Table.Hand.Wagers = 2
	call := uint64(50)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call}
	if a := b.Decide(d); a.Kind != wire.ActionCall {
		t.Fatalf("overfolded useful low cards: %+v", a)
	}
}

func TestSnowContinuesOnlyAgainstDrawingOpponent(t *testing.T) {
	original := snowProfile
	snowProfile = "baseline"
	t.Cleanup(func() { snowProfile = original })
	b := setup("2c", "2d", "3h", "3s", "Kc")
	b.Table.Hand.Street = table.Draw2
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 2})
	draw := wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5}
	if a := b.propose(draw, 0); len(a.Cards) != 0 || !b.snow {
		t.Fatalf("did not start blocker snow: %+v", a)
	}
	bet := wire.Decision{Kind: wire.DecisionWager, Check: true}
	if a := b.propose(bet, 0.9); a.Kind != wire.ActionRaise {
		t.Fatalf("snow did not follow through: %+v", a)
	}
	b.Table.Hand.Street = table.Draw3
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
	if a := b.propose(draw, 0.9); len(a.Cards) == 0 || b.snow {
		t.Fatalf("continued snow into a pat: %+v", a)
	}
	b.HandStart(wire.Message{Seat: 1})
	if b.snow {
		t.Fatal("snow leaked into next hand")
	}
}

func TestRangeEquityCountsTiesAndOrdering(t *testing.T) {
	r := makeRange([]weightedValue{{100, 1}, {200, 2}, {300, 1}})
	for _, tc := range []struct {
		v    uint32
		want float64
	}{{50, 0}, {100, .125}, {200, .5}, {300, .875}, {400, 1}} {
		if got := r.equity(deuce.Value(tc.v)); got != tc.want {
			t.Fatalf("equity(%d)=%f want %f", tc.v, got, tc.want)
		}
	}
}

func TestDrawEnumerationExcludesOurDiscardedCards(t *testing.T) {
	b := setup("2c", "3d", "4h", "5s", "Ac")
	b.Table.Hand.Street = table.Draw3
	b.muck = cards.NewSet(cards.MustParse("7c", "7d", "7h", "7s"))
	r := makeRange([]weightedValue{{uint32(deuce.Eval(cards.MustParse("2d", "3h", "4s", "6c", "8d"))), 1}})
	discard := cards.MustParse("Ac")
	blocked := b.drawEquity(discard, r)
	b.muck = 0
	open := b.drawEquity(discard, r)
	if open <= blocked {
		t.Fatalf("blocking all sevens did not reduce equity: %f <= %f", open, blocked)
	}
}

func TestRiverAceCatchesMissesWhenPotPaysTenToOne(t *testing.T) {
	b := setup("2c", "3d", "4h", "7s", "Ac")
	b.Table.Hand.Street = table.Draw3
	b.Table.Hand.Wagers = 1
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 0, Count: 1})
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 1})
	b.pot = 1000
	call := uint64(100)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call}
	if a := b.Decide(d); a.Kind != wire.ActionCall {
		t.Fatalf("overfolded profitable catcher: %+v", a)
	}
}

func TestLastDrawSnowUsesCurrentOpponentDraw(t *testing.T) {
	original := snowProfile
	snowProfile = "baseline"
	t.Cleanup(func() { snowProfile = original })
	b := setup("2c", "3d", "Th", "Ks", "Ac")
	b.Table.Hand.Street = table.Draw3
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 1})
	a := b.propose(wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5}, 0.5)
	if len(a.Cards) != 0 || !b.snow {
		t.Fatalf("missed final-draw bluff: %+v", a)
	}
}

func TestImprovedDrawBetsAgainstTwoCardDraw(t *testing.T) {
	b := setup("2c", "3d", "4h", "7s", "Kc")
	b.Table.Hand.Street = table.Draw1
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 0, Count: 2})
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 2})
	a := b.propose(wire.Decision{Kind: wire.DecisionWager, Check: true}, 0.9)
	if a.Kind != wire.ActionRaise {
		t.Fatalf("did not bet improved draw: %+v", a)
	}
}

func TestTwoCardDrawContinuesAtGoodPriceAgainstDraw(t *testing.T) {
	b := setup("2c", "3d", "4h", "Ks", "Ac")
	b.Table.Hand.Street = table.Draw2
	b.Table.Hand.Wagers = 1
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 2})
	b.pot = 500
	call := uint64(100)
	a := b.Decide(wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call})
	if a.Kind != wire.ActionCall {
		t.Fatalf("overfolded live draw: %+v", a)
	}
}

func TestRememberRaiseWhenOpponentKeepsPatting(t *testing.T) {
	b := setup("2c", "3d", "4h", "6s", "8c")
	b.Table.Hand.Street = table.Draw1
	b.Observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Raise(100), StreetCommit: 100})
	b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: table.Draw2})
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
	b.Observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Bet(100), StreetCommit: 100})
	b.pot = 700
	call := uint64(100)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call, Raise: &wire.Range{MinTo: 200, MaxTo: 200}}
	if a := b.Decide(d); a.Kind != wire.ActionCall {
		t.Fatalf("forgot earlier pat raise: %+v", a)
	}
	b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: table.Draw3})
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 1})
	if b.strongPat {
		t.Fatal("range was not released after opponent broke pat")
	}
}

func TestRoughSevenDoesNotCapPatRaise(t *testing.T) {
	b := setup("2c", "4d", "5h", "6s", "7c")
	b.Table.Hand.Street = table.Draw3
	b.Table.Hand.Wagers = 2
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
	b.pot = 900
	call := uint64(100)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call, Raise: &wire.Range{MinTo: 300, MaxTo: 300}}
	if a := b.Decide(d); a.Kind != wire.ActionCall {
		t.Fatalf("capped rough seven: %+v", a)
	}
}

func TestAlwaysRaiserUsesBroadValuePolicy(t *testing.T) {
	b := setup("2c", "3d", "4h", "6s", "8c")
	b.Table.Hand.Street = table.Draw3
	for i := 0; i < 80; i++ {
		b.Observe(wire.Event{Kind: wire.EventActed, Seat: 1, Action: wire.Raise(100)})
	}
	b.HandStart(wire.Message{Seat: 0})
	b.Table.Hand.Cards = cards.MustParse("2c", "3d", "4h", "6s", "8c")
	b.Table.Hand.Street = table.Draw3
	b.strongPat = true
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
	d := wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: &wire.Range{MinTo: 100, MaxTo: 100}}
	if a := b.Decide(d); a.Kind != wire.ActionBet {
		t.Fatalf("missed value against an observed always-raiser: %+v", a)
	}
	b.Hello(wire.Message{SeatCount: 2})
	if a := b.Decide(d); a.Kind != wire.ActionCheck {
		t.Fatalf("opponent statistics leaked across matches: %+v", a)
	}
}
