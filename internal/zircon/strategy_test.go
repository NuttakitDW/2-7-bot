package zircon

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func strategyBot(hero int, hand ...string) *Bot {
	b := New()
	b.State = testState(hero)
	b.State.Hand.Cards = cards.MustParse(hand...)
	return b
}

func openDecision() wire.Decision {
	return wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: &wire.Range{MinTo: 200, MaxTo: 200}}
}
func faceDecision(call uint64, raise bool) wire.Decision {
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call}
	if raise {
		d.Raise = &wire.Range{MinTo: 300, MaxTo: 300}
	}
	return d
}

func TestLateCleanTwoDrawOpensButEarlyFolds(t *testing.T) {
	early := strategyBot(3, "3c", "5d", "8h", "Qs", "Kc")
	late := strategyBot(5, "3c", "5d", "8h", "Qs", "Kc")
	if got := early.Decide(openDecision()).Kind; got != wire.ActionCheck {
		t.Fatalf("UTG=%s", got)
	}
	if got := late.Decide(openDecision()).Kind; got != wire.ActionBet {
		t.Fatalf("cutoff=%s", got)
	}
}

func TestFirstInCallAmountStillRaisesPlayableHand(t *testing.T) {
	b := strategyBot(3, "2c", "4d", "7h", "Qs", "Kc")
	if got := b.Decide(faceDecision(100, true)).Kind; got != wire.ActionRaise {
		t.Fatalf("first-in with blind call=%s", got)
	}
}

func TestBigBlindDiscountDoesNotApplyToColdReraise(t *testing.T) {
	bb := strategyBot(2, "2c", "4d", "7h", "Qs", "Kc")
	bb.State.Hand.Pot = 500
	bb.State.Hand.Seats[0].Contribution = 200
	bb.State.Hand.Seats[1].Contribution = 100
	bb.State.Hand.Seats[2].Contribution = 100
	bb.State.Hand.Seats[3].Contribution = 100
	bb.State.Hand.StreetAggressions = 1
	if got := bb.Decide(faceDecision(100, true)).Kind; got != wire.ActionCall {
		t.Fatalf("BB single raise=%s", got)
	}
	bb.State.Hand.StreetAggressions = 2
	if got := bb.Decide(faceDecision(100, true)).Kind; got != wire.ActionFold {
		t.Fatalf("BB cold reraise=%s", got)
	}
	bb.State.Hand.HeroPredrawRaises = 1
	if got := bb.Decide(faceDecision(100, true)).Kind; got != wire.ActionCall {
		t.Fatalf("own open priced defense=%s", got)
	}
}

func TestFinalDrawNineNeedsFreshLivePatAndUsefulRedraw(t *testing.T) {
	b := strategyBot(0, "2c", "3d", "4h", "6s", "9c")
	b.State.Hand.Street = Draw3
	for seat := 2; seat < SeatCount; seat++ {
		b.State.Hand.Seats[seat].Folded = true
	}
	d := wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5}
	if got := b.Decide(d); len(got.Cards) != 0 {
		t.Fatalf("drawing opponent broke nine: %v", got.Cards)
	}
	b.State.Hand.Seats[1].Draws[Draw3] = 0
	if got := b.Decide(d); len(got.Cards) != 1 || got.Cards[0].Rank != cards.Nine {
		t.Fatalf("pat pressure discard=%v", got.Cards)
	}
	b.State.Hand.Seats[1].Folded = true
	if got := b.Decide(d); len(got.Cards) != 0 {
		t.Fatalf("folded pat broke nine: %v", got.Cards)
	}
}

func TestRoughMadeNineDoesNotFallIntoStructuralTwoCardDraw(t *testing.T) {
	b := strategyBot(0, "4c", "5d", "6h", "7s", "9c")
	b.State.Hand.Street = Draw3
	for seat := 2; seat < SeatCount; seat++ {
		b.State.Hand.Seats[seat].Folded = true
	}
	b.State.Hand.Seats[1].Draws[Draw3] = 0
	if got := b.Decide(wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5}); len(got.Cards) != 0 {
		t.Fatalf("rough nine discarded=%v", got.Cards)
	}
}

func TestExactEnumerationIncludesAllUnseenCardsAndBrokenResults(t *testing.T) {
	hand := cards.MustParse("2c", "3c", "4c", "6c", "9d")
	out := EnumerateOneCard(hand, hand[4])
	if out.Total != 47 {
		t.Fatalf("total=%d", out.Total)
	}
	if out.Seven+out.Eight+out.Nine >= out.Total {
		t.Fatalf("enumeration omitted pairs/straights/flushes: %+v", out)
	}
}

func TestRiverMultiwayRoughEightStricterThanHeadsUpSmoothEight(t *testing.T) {
	smooth := strategyBot(0, "2c", "3d", "4h", "6s", "8c")
	smooth.State.Hand.Street = Draw3
	for seat := 2; seat < SeatCount; seat++ {
		smooth.State.Hand.Seats[seat].Folded = true
	}
	smooth.State.Hand.Seats[1].Draws[Draw3] = 0
	if got := smooth.Decide(faceDecision(200, false)).Kind; got != wire.ActionCall {
		t.Fatalf("HU smooth eight=%s", got)
	}
	rough := strategyBot(0, "3c", "4d", "5h", "7s", "8c")
	rough.State.Hand.Street = Draw3
	rough.State.Hand.StreetAggressions = 2
	for seat := 1; seat <= 3; seat++ {
		rough.State.Hand.Seats[seat].Draws[Draw3] = 0
	}
	for seat := 4; seat < SeatCount; seat++ {
		rough.State.Hand.Seats[seat].Folded = true
	}
	if got := rough.Decide(faceDecision(200, false)).Kind; got != wire.ActionFold {
		t.Fatalf("multiway rough eight=%s", got)
	}
}

func TestNutsNeverFoldButOrdinarySevenDoesNotAutoRaisePressure(t *testing.T) {
	nuts := strategyBot(0, "2c", "3d", "4h", "5s", "7c")
	nuts.State.Hand.Street = Draw3
	nuts.State.Hand.StreetAggressions = 3
	if got := nuts.Decide(faceDecision(200, true)).Kind; got != wire.ActionRaise {
		t.Fatalf("nuts=%s", got)
	}
	seven := strategyBot(0, "2c", "3d", "4h", "6s", "7c")
	seven.State.Hand.Street = Draw3
	seven.State.Hand.StreetAggressions = 2
	if got := seven.Decide(faceDecision(200, true)).Kind; got != wire.ActionCall {
		t.Fatalf("ordinary seven=%s", got)
	}
}

func TestRiverStrongSevenRaisesOneWagerButRoughSevenOnlyCalls(t *testing.T) {
	strong := strategyBot(0, "2c", "3d", "5h", "6s", "7c")
	strong.State.Hand.Street = Draw3
	for seat := 2; seat < SeatCount; seat++ {
		strong.State.Hand.Seats[seat].Folded = true
	}
	strong.State.Hand.Seats[1].Draws[Draw3] = 0
	strong.State.Hand.Seats[1].ActedOnStreet = true
	strong.State.Hand.StreetAggressions = 1
	strong.State.Hand.LastAggressor = 1
	if got := strong.Decide(faceDecision(200, true)).Kind; got != wire.ActionRaise {
		t.Fatalf("strong seven=%s", got)
	}
	rough := strategyBot(0, "2c", "4d", "5h", "6s", "7c")
	rough.State.Hand.Street = Draw3
	for seat := 2; seat < SeatCount; seat++ {
		rough.State.Hand.Seats[seat].Folded = true
	}
	rough.State.Hand.Seats[1].Draws[Draw3] = 0
	rough.State.Hand.Seats[1].ActedOnStreet = true
	rough.State.Hand.StreetAggressions = 1
	rough.State.Hand.LastAggressor = 1
	if got := rough.Decide(faceDecision(200, true)).Kind; got != wire.ActionCall {
		t.Fatalf("rough seven=%s", got)
	}
}

func TestRiverNineClosingCallsRespectFieldAndPrice(t *testing.T) {
	b := strategyBot(0, "2c", "4d", "6h", "8s", "9c")
	b.State.Hand.Street = Draw3
	for seat := 2; seat < SeatCount; seat++ {
		b.State.Hand.Seats[seat].Folded = true
	}
	b.State.Hand.Seats[1].Draws[Draw3] = 2
	b.State.Hand.Seats[1].ActedOnStreet = true
	b.State.Hand.StreetAggressions = 1
	b.State.Hand.LastAggressor = 1
	b.State.Hand.Seats[0].Contribution = 400
	b.State.Hand.Seats[1].Contribution = 600
	b.State.Hand.Pot = 1000
	if got := b.Decide(faceDecision(200, false)).Kind; got != wire.ActionCall {
		t.Fatalf("priced HU nine=%s", got)
	}
	b.State.Hand.Pot = 200
	b.State.Hand.Seats[0].Contribution = 0
	b.State.Hand.Seats[1].Contribution = 200
	if got := b.Decide(faceDecision(200, false)).Kind; got != wire.ActionFold {
		t.Fatalf("expensive HU nine=%s", got)
	}
	b.State.Hand.Pot = 1000
	b.State.Hand.Seats[0].Contribution = 400
	b.State.Hand.Seats[1].Contribution = 600
	b.State.Hand.Seats[1].Draws[Draw3] = 0
	if got := b.Decide(faceDecision(200, false)).Kind; got != wire.ActionFold {
		t.Fatalf("nine vs live pat=%s", got)
	}
	multi := strategyBot(0, "2c", "4d", "5h", "7s", "9c")
	multi.State.Hand.Street = Draw3
	for seat := 3; seat < SeatCount; seat++ {
		multi.State.Hand.Seats[seat].Folded = true
	}
	for seat := 1; seat <= 2; seat++ {
		multi.State.Hand.Seats[seat].Draws[Draw3] = 1
		multi.State.Hand.Seats[seat].ActedOnStreet = true
	}
	multi.State.Hand.StreetAggressions = 1
	multi.State.Hand.LastAggressor = 1
	multi.State.Hand.Seats[0].Contribution = 400
	multi.State.Hand.Seats[1].Contribution = 500
	multi.State.Hand.Seats[2].Contribution = 500
	multi.State.Hand.Pot = 1400
	if got := multi.Decide(faceDecision(100, false)).Kind; got != wire.ActionCall {
		t.Fatalf("priced three-way smooth nine=%s", got)
	}
	multi.State.Hand.Cards = cards.MustParse("2c", "5d", "6h", "8s", "9c")
	if got := multi.Decide(faceDecision(100, false)).Kind; got != wire.ActionFold {
		t.Fatalf("three-way rough nine=%s", got)
	}
}

func TestRiverTenCallsOnlyCleanClosingTwoDrawSpot(t *testing.T) {
	b := strategyBot(0, "2c", "4d", "6h", "8s", "Tc")
	b.State.Hand.Street = Draw3
	for seat := 2; seat < SeatCount; seat++ {
		b.State.Hand.Seats[seat].Folded = true
	}
	b.State.Hand.Seats[1].Draws[Draw3] = 2
	b.State.Hand.Seats[1].ActedOnStreet = true
	b.State.Hand.StreetAggressions = 1
	b.State.Hand.LastAggressor = 1
	b.State.Hand.Seats[0].Contribution = 500
	b.State.Hand.Seats[1].Contribution = 600
	b.State.Hand.Pot = 1100
	if got := b.Decide(faceDecision(100, false)).Kind; got != wire.ActionCall {
		t.Fatalf("ten vs two draw=%s", got)
	}
	b.State.Hand.Seats[1].Draws[Draw2] = 0
	if got := b.Decide(faceDecision(100, false)).Kind; got != wire.ActionFold {
		t.Fatalf("ten vs prior pat=%s", got)
	}
}

func TestPostdrawCallUsesPriceAndKeepsInvestedGoodDraw(t *testing.T) {
	b := strategyBot(0, "2c", "3d", "4h", "6s", "Kc")
	b.State.Hand.Street = Draw2
	b.State.Hand.Pot = 900
	b.State.Hand.StreetAggressions = 1
	for seat := 2; seat < SeatCount; seat++ {
		b.State.Hand.Seats[seat].Folded = true
	}
	b.State.Hand.Seats[0].Contribution = 400
	b.State.Hand.Seats[1].Contribution = 500
	b.State.Hand.Seats[1].Draws[Draw2] = 1
	if got := b.Decide(faceDecision(100, false)).Kind; got != wire.ActionCall {
		t.Fatalf("priced good draw=%s", got)
	}
	b.State.Hand.Pot = 100
	b.State.Hand.Seats[0].Contribution = 0
	b.State.Hand.Seats[1].Contribution = 100
	if got := b.Decide(faceDecision(100, false)).Kind; got != wire.ActionFold {
		t.Fatalf("expensive draw=%s", got)
	}
}

func TestStrongOneCardDrawCanContinueAgainstSinglePatAtPrice(t *testing.T) {
	b := strategyBot(0, "3c", "4d", "5h", "8s", "Kc")
	b.State.Hand.Street = Draw1
	b.State.Hand.Pot = 1000
	b.State.Hand.StreetAggressions = 1
	for seat := 2; seat < SeatCount; seat++ {
		b.State.Hand.Seats[seat].Folded = true
	}
	b.State.Hand.Seats[0].Contribution = 500
	b.State.Hand.Seats[1].Contribution = 500
	b.State.Hand.Seats[1].Draws[Draw1] = 0
	if got := b.Decide(faceDecision(100, false)).Kind; got != wire.ActionCall {
		t.Fatalf("strong draw vs one pat=%s", got)
	}
	b.State.Hand.Seats[2].Folded = false
	b.State.Hand.Seats[2].Draws[Draw1] = 0
	if got := b.Decide(faceDecision(100, false)).Kind; got != wire.ActionFold {
		t.Fatalf("strong draw vs two pats=%s", got)
	}
}

func TestStrongDrawLeadsWhenEveryLiveOpponentDrewAtLeastTwo(t *testing.T) {
	b := strategyBot(0, "3c", "4d", "5h", "8s", "Kc")
	b.State.Hand.Street = Draw1
	for seat := 1; seat < SeatCount; seat++ {
		b.State.Hand.Seats[seat].Draws[Draw1] = 2
	}
	if got := b.Decide(openDecision()).Kind; got != wire.ActionBet {
		t.Fatalf("strong draw lead=%s", got)
	}
	b.State.Hand.Seats[3].Draws[Draw1] = 1
	if got := b.Decide(openDecision()).Kind; got != wire.ActionCheck {
		t.Fatalf("lead vs one-card draw=%s", got)
	}
}

func TestSmoothNineBreaksEarlyAgainstFreshPat(t *testing.T) {
	b := strategyBot(0, "2c", "3d", "4h", "6s", "9c")
	b.State.Hand.Street = Draw1
	for seat := 2; seat < SeatCount; seat++ {
		b.State.Hand.Seats[seat].Folded = true
	}
	b.State.Hand.Seats[1].Draws[Draw1] = 0
	if got := b.Decide(wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5}); len(got.Cards) != 1 || got.Cards[0].Rank != cards.Nine {
		t.Fatalf("early nine redraw=%v", got.Cards)
	}
}

func TestPendingShortCallUsesOnlyContestablePot(t *testing.T) {
	b := strategyBot(0, "2c", "3d", "4h", "7s", "Kc")
	b.State.Hand.Pot = 400
	b.State.Hand.Seats[1].Contribution = 200
	b.State.Hand.Seats[2].Contribution = 200
	call := uint64(50)
	if b.affordable(&call, 7) {
		t.Fatal("short call priced against chips hero cannot contest")
	}
}
