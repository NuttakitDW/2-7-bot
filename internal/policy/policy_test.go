package policy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/table"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// spot describes a decision point. It is realised by driving the real event
// decoder rather than by setting fields, so these tests exercise the same
// path a match does — including the predraw ordering where the blinds are
// posted before street-start.
type spot struct {
	seat      int         // 0 is the button; heads-up it posts the small blind
	street    int         //
	hole      string      // five cards, e.g. "7c5d4h3s2c"
	oppDraws  map[int]int // street -> the opponent's draw count
	oppWagers int         // bets or raises the opponent made this street
}

func (s spot) build(t *testing.T) *table.Table {
	t.Helper()
	state := table.New()
	state.Hello(wire.Message{
		Type: wire.MsgHello, GameID: "27td-fl", SeatCount: 2, StartingStack: 10000,
		Stakes: wire.Stakes{Kind: "blinds", SmallBlind: 50, BigBlind: 100},
	})
	state.HandStart(wire.Message{Type: wire.MsgHandStart, HandNo: 1, Seat: s.seat})
	state.Observe(wire.Event{Kind: wire.EventHandStart, Button: 0})
	state.Observe(wire.Event{Kind: wire.EventPost, Seat: 0, PostKind: "small-blind", Amount: 50})
	state.Observe(wire.Event{Kind: wire.EventPost, Seat: 1, PostKind: "big-blind", Amount: 100})
	state.Observe(wire.Event{Kind: wire.EventStreetStart, Street: table.Predraw, Label: "predraw"})
	state.Observe(wire.Event{
		Kind: wire.EventDealHole, Seat: s.seat, Cards: parseHand(t, s.hole), Count: 5,
	})

	opponent := 1 - s.seat
	for street := table.Draw1; street <= s.street; street++ {
		state.Observe(wire.Event{
			Kind: wire.EventStreetStart, Street: street, Label: fmt.Sprintf("draw%d", street),
		})
		if count, ok := s.oppDraws[street]; ok {
			state.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: opponent, Count: count})
		}
	}
	for i := 0; i < s.oppWagers; i++ {
		state.Observe(wire.Event{
			Kind: wire.EventActed, Seat: opponent, Action: wire.Action{Kind: wire.ActionRaise},
		})
	}
	return state
}

func parseHand(t *testing.T, text string) []cards.Card {
	t.Helper()
	hand := make([]cards.Card, 0, 5)
	for i := 0; i+1 < len(text); i += 2 {
		card, err := cards.ParseCard(text[i : i+2])
		if err != nil {
			t.Fatalf("hand %q: %v", text, err)
		}
		hand = append(hand, card)
	}
	return hand
}

func ranks(list []cards.Rank) string {
	out := make([]string, len(list))
	for i, rank := range list {
		out[i] = rank.String()
	}
	return strings.Join(out, "")
}

// The structural half of the chart: shapes and keep lists. Cases are chosen
// to pin down what a naive "keep the lowest cards" rule gets wrong: straight
// shapes, flush shapes, pairs, and the deuce. The Open and Defend columns
// are generated data and are tested by property below, not by case.
func TestClassify(t *testing.T) {
	tests := []struct {
		name  string
		hand  string
		shape Shape
		keep  string
	}{
		{"the nuts stand pat", "7c5d4h3s2c", Pat, "23457"},
		{"a made eight stands pat", "8c5d4h3s2c", Pat, "23458"},
		{"a rough nine is pat", "9c8d7h5s2c", Pat, "25789"},

		{"four low cards draw one", "2c3d4h7sKc", OneCardDraw, "2347"},
		// A nine low is a made hand; A-9-4-3-2 is not. The nine is above
		// the eight-or-better target, so it goes with the ace and the
		// hand draws two rather than one.
		{"an ace-high hand draws two rather than one", "Ac9d4h3s2c", TwoCardDraw, "234"},

		// Straights lose to any nine low, so a four-card run that two
		// ranks complete is not a draw worth taking.
		{"an open-ended four-straight is not a one-card draw", "3c4d5h6sKc", TwoCardDraw, "345"},
		// 2-3-4-5 is only a one-ender: a six makes the straight, a seven
		// makes the nuts. It stays a one-card draw.
		{"2345 is a one-ender and still draws one", "2c3d4h5sKc", OneCardDraw, "2345"},
		// The same hand suited is a made six-high straight flush shape;
		// breaking it is right, and the straight is what forbids standing.
		{"a six-high straight is broken back to a one-card draw", "2c3d4h5s6c", OneCardDraw, "2345"},
		{"a flush is broken back to a one-card draw", "7c5c4c3c2c", OneCardDraw, "2345"},

		// Pairs are dead weight: the second copy of a rank can never
		// contribute to a low.
		{"a pair leaves a four-card draw behind it", "2c2d4h5s7c", OneCardDraw, "2457"},

		// 4-5-6, 5-6-7 and 6-7-8 are the shapes that make unwanted
		// straights from both ends, and no deuce means no draw to the
		// nuts either. The salvage keep must not hand the same shape
		// back, so it keeps two cards rather than three.
		{"a middle three-straight is not a draw at all", "5c6d7hKsQc", Junk, "56"},
		{"a deuce and a low card draw three", "2c7dKhQsJc", ThreeCardDraw, "27"},
		{"no deuce and no low cards is junk", "AcKdQhJs9c", Junk, "9"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			chart := Classify(parseHand(t, test.hand))
			if chart.Shape != test.shape {
				t.Errorf("shape = %v, want %v", chart.Shape, test.shape)
			}
			if test.keep != "" && ranks(chart.Keep) != test.keep {
				t.Errorf("keep = %q, want %q", ranks(chart.Keep), test.keep)
			}
		})
	}
}

// The structural three-bet rule survives the generated table: the top of
// the defending range raises, the rest flats. These hands sit far enough up
// any candidate table's ranking that their continue bit is not in question.
func TestDefendKeepsTheStructuralThreeBet(t *testing.T) {
	tests := []struct {
		name   string
		hand   string
		defend Move
	}{
		{"the nuts three-bet", "7c5d4h3s2c", Raise},
		{"a smooth nine three-bets", "9c6d4h3s2c", Raise},
		{"a clean one-card draw three-bets", "2c3d4h7sKc", Raise},
		{"a rough nine flats rather than three-betting", "9c8d7h5s2c", Call},
		{"a straight-risk one-card draw flats", "2c3d4h5sKc", Call},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Classify(parseHand(t, test.hand)).Defend; got != test.defend {
				t.Errorf("defend = %v, want %v", got, test.defend)
			}
		})
	}
}

// The properties any generated table must satisfy, whatever realization
// factor produced it. The 2026-08-30 benchmarks measured h1's ~50% button
// open as its dominant leak, so a candidate that is not clearly wider than
// that is a generation bug, not a strategy choice.
func TestChartTableProperties(t *testing.T) {
	openWeight, defendWeight, total := 0.0, 0.0, 0.0
	for id := handclass.ID(0); id < handclass.Num; id++ {
		weight := float64(handclass.Weight(id))
		if weight == 0 {
			continue
		}
		chart := Classify(handclass.Representative(id))
		if chart.Open == Raise {
			openWeight += weight
		}
		if chart.Defend != Fold {
			defendWeight += weight
		}
		total += weight
	}

	if frac := openWeight / total; frac < 0.55 {
		t.Errorf("button opens %.1f%% of deals, want clearly wider than h1's ~50%%", 100*frac)
	}
	if frac := defendWeight / total; frac < 0.30 {
		t.Errorf("big blind defends %.1f%% of deals, want at least 30%%", 100*frac)
	}

	if chart := Classify(parseHand(t, "7c5d4h3s2c")); chart.Open != Raise {
		t.Error("the nuts must open")
	}
	// The bottom of the deck folds the button — but it may defend the big
	// blind. The 2026-09-01 candidate sweep measured the defend-everything
	// shape as the winner (getting 3:1 closing odds, with the postdraw
	// rules capping the damage), so no floor is asserted on defends here.
	if chart := Classify(parseHand(t, "AcAdKhKsQc")); chart.Open != Fold {
		t.Error("aces up — the bottom of the deck — must fold the button")
	}
}

// The break rule — the one decision in h1 that reads the opponent's public
// draw count rather than just its own cards.
func TestDrawUsesTheOpponentsDrawCount(t *testing.T) {
	tests := []struct {
		name     string
		hole     string
		oppDrew  int
		noRead   bool
		wantPat  bool
		wantDrop string
	}{
		{name: "a seven stands pat against a pat opponent", hole: "7c5d4h3s2c", oppDrew: 0, wantPat: true},
		{name: "an eight stands pat against a one-card draw", hole: "8c5d4h3s2c", oppDrew: 1, wantPat: true},

		// The rule itself, in both directions.
		{name: "a nine stands pat against a two-card draw", hole: "9c8d7h5s2c", oppDrew: 2, wantPat: true},
		{name: "a nine breaks against a one-card draw", hole: "9c8d7h5s2c", oppDrew: 1, wantDrop: "9c"},
		{name: "a nine breaks against a pat opponent", hole: "9c8d7h5s2c", oppDrew: 0, wantDrop: "9c"},
		{name: "a nine stands pat with no read yet", hole: "9c8d7h5s2c", noRead: true, wantPat: true},

		// A ten is only worth standing on when the opponent is far behind.
		{name: "a ten stands pat against a three-card draw", hole: "Tc8d7h5s2c", oppDrew: 3, wantPat: true},
		{name: "a ten breaks against a one-card draw", hole: "Tc8d7h5s2c", oppDrew: 1, wantDrop: "Tc"},
		{name: "a ten breaks with no read yet", hole: "Tc8d7h5s2c", noRead: true, wantDrop: "Tc"},

		// A hand that is not a low never stands, whatever they drew.
		{name: "a pair breaks, and the dead copy is what goes", hole: "2c2d4h5s7c", oppDrew: 4, wantDrop: "2d"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spot := spot{seat: 0, street: table.Draw2, hole: test.hole}
			if !test.noRead {
				spot.oppDraws = map[int]int{table.Draw2: test.oppDrew}
			}
			state := spot.build(t)
			discards := Draw(&state.Hand)

			if test.wantPat {
				if len(discards) != 0 {
					t.Fatalf("discarded %v, want a stand pat", cards.Strings(discards))
				}
				return
			}
			if got := strings.Join(cards.Strings(discards), ""); got != test.wantDrop {
				t.Errorf("discarded %q, want %q", got, test.wantDrop)
			}
		})
	}
}

// Predraw is raise-or-fold from the chart, and which column applies depends
// on whether the pot has been opened. That distinction is what separates the
// button's opening range from its defending range, and getting it wrong turns
// an opener into a limper.
func TestPredrawUsesTheRightColumn(t *testing.T) {
	facingBlind := wire.Decision{
		Kind: wire.DecisionWager, Fold: true,
		Call: ptr(uint64(50)), Raise: &wire.Range{MinTo: 200, MaxTo: 200},
	}
	facingRaise := wire.Decision{
		Kind: wire.DecisionWager, Fold: true,
		Call: ptr(uint64(100)), Raise: &wire.Range{MinTo: 300, MaxTo: 300},
	}
	bigBlindOption := wire.Decision{Kind: wire.DecisionWager, Check: true}

	tests := []struct {
		name     string
		spot     spot
		decision wire.Decision
		want     string
	}{
		{"the button opens its range rather than limping",
			spot{seat: 0, hole: "2c3d4h7sKc"}, facingBlind, wire.ActionRaise},
		{"the button folds the bottom of the deck",
			spot{seat: 0, hole: "AcAdKhKsQc"}, facingBlind, wire.ActionFold},
		{"the big blind three-bets its strongest defends",
			spot{seat: 1, hole: "7c5d4h3s2c", oppWagers: 1}, facingRaise, wire.ActionRaise},
		{"the big blind calls a rough pat nine rather than three-betting",
			spot{seat: 1, hole: "9c8d7h5s2c", oppWagers: 1}, facingRaise, wire.ActionCall},
		// The generated chart defends the whole big blind at 3:1 closing
		// odds — measured as the winning shape in the 2026-09-01 sweep —
		// so even the bottom of the deck calls rather than folds here.
		{"the big blind defends even the bottom of the deck",
			spot{seat: 1, hole: "AcAdKhKsQc", oppWagers: 1}, facingRaise, wire.ActionCall},
		// Folding for free is never legal, so junk with the option checks.
		{"the big blind checks its option with the bottom of the deck",
			spot{seat: 1, hole: "AcAdKhKsQc"}, bigBlindOption, wire.ActionCheck},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := test.spot.build(t)
			if got := Decide(state, test.decision).Kind; got != test.want {
				t.Errorf("action = %q, want %q", got, test.want)
			}
		})
	}
}

// The betting ladder after a draw. Both of this repo's retired bots died
// here: one bet hands that could not win, the other called with them.
func TestDrawStreetBetting(t *testing.T) {
	open := wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: &wire.Range{MinTo: 200, MaxTo: 200}}
	facingBet := wire.Decision{
		Kind: wire.DecisionWager, Fold: true,
		Call: ptr(uint64(200)), Raise: &wire.Range{MinTo: 400, MaxTo: 400},
	}
	capped := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: ptr(uint64(200))}

	tests := []struct {
		name     string
		spot     spot
		decision wire.Decision
		want     string
	}{
		{"a seven bets", spot{street: table.Draw2, hole: "7c5d4h3s2c",
			oppDraws: map[int]int{table.Draw2: 0}}, open, wire.ActionBet},
		{"a seven raises", spot{street: table.Draw2, hole: "7c5d4h3s2c",
			oppDraws: map[int]int{table.Draw2: 0}, oppWagers: 1}, facingBet, wire.ActionRaise},
		{"a seven calls once the cap removes the raise", spot{street: table.Draw2,
			hole: "7c5d4h3s2c", oppDraws: map[int]int{table.Draw2: 0}, oppWagers: 4},
			capped, wire.ActionCall},

		{"an eight bets into a pat opponent", spot{street: table.Draw2, hole: "8c5d4h3s2c",
			oppDraws: map[int]int{table.Draw2: 0}}, open, wire.ActionBet},

		{"a nine bets when the opponent drew two", spot{street: table.Draw2,
			hole: "9c8d7h5s2c", oppDraws: map[int]int{table.Draw2: 2}}, open, wire.ActionBet},
		{"a nine only checks when the opponent drew one", spot{street: table.Draw2,
			hole: "9c8d7h5s2c", oppDraws: map[int]int{table.Draw2: 1}}, open, wire.ActionCheck},
		{"a nine still calls a bet", spot{street: table.Draw2, hole: "9c8d7h5s2c",
			oppDraws: map[int]int{table.Draw2: 1}, oppWagers: 1}, facingBet, wire.ActionCall},

		// A marginal made hand checks rather than bets, calls one bet, and
		// stops once the big bets are raised.
		{"a ten checks rather than betting", spot{street: table.Draw3, hole: "Tc8d7h5s2c",
			oppDraws: map[int]int{table.Draw3: 3}}, open, wire.ActionCheck},
		{"a ten calls one big bet", spot{street: table.Draw3, hole: "Tc8d7h5s2c",
			oppDraws: map[int]int{table.Draw3: 1}, oppWagers: 1}, facingBet, wire.ActionCall},
		{"a ten folds to a raised big bet", spot{street: table.Draw3, hole: "Tc8d7h5s2c",
			oppDraws: map[int]int{table.Draw3: 1}, oppWagers: 2}, facingBet, wire.ActionFold},

		// The calling-station guard: a missed hand pays only while there
		// is still a draw left to pay for.
		{"a one-card draw calls a small bet on draw1", spot{street: table.Draw1,
			hole: "2c3d4h7sKc", oppDraws: map[int]int{table.Draw1: 1}, oppWagers: 1},
			facingBet, wire.ActionCall},
		{"a three-card draw folds to a bet on draw2", spot{street: table.Draw2,
			hole: "2c7dKhQsJc", oppDraws: map[int]int{table.Draw2: 1}, oppWagers: 1},
			facingBet, wire.ActionFold},
		{"a missed hand folds after the last draw", spot{street: table.Draw3,
			hole: "AcKdQhJs9c", oppDraws: map[int]int{table.Draw3: 1}, oppWagers: 1},
			facingBet, wire.ActionFold},
		{"a missed hand checks after the last draw when it is free",
			spot{street: table.Draw3, hole: "AcKdQhJs9c",
				oppDraws: map[int]int{table.Draw3: 1}}, open, wire.ActionCheck},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := test.spot.build(t)
			if got := Decide(state, test.decision).Kind; got != test.want {
				t.Errorf("action = %q, want %q", got, test.want)
			}
		})
	}
}

// Decide must answer a draw decision with a legal discard even when the hand
// is not yet readable, and must never propose more than max_discards.
func TestDecideAlwaysAnswersADraw(t *testing.T) {
	state := spot{seat: 0, street: table.Draw1, hole: "AcKdQhJs9c"}.build(t)
	action := Decide(state, wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5})
	if action.Kind != wire.ActionDiscard {
		t.Fatalf("kind = %q, want discard", action.Kind)
	}
	if len(action.Cards) > 5 {
		t.Errorf("discarded %d cards, want at most 5", len(action.Cards))
	}

	t.Run("a draw cap is respected", func(t *testing.T) {
		action := Decide(state, wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 2})
		if len(action.Cards) != 2 {
			t.Errorf("discarded %v, want exactly 2", cards.Strings(action.Cards))
		}
	})
}

func ptr[T any](value T) *T { return &value }
