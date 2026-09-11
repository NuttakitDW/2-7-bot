package sixmax

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/sixmaxclone"
	"github.com/nuttakit/2-7-bot/internal/sixmaxrange"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func wager(facing, aggressive bool) wire.Decision {
	d := wire.Decision{Kind: wire.DecisionWager, Fold: facing, Check: !facing}
	call, to := uint64(100), wire.Range{MinTo: 200, MaxTo: 200}
	if facing {
		d.Call = &call
		if aggressive {
			d.Raise = &to
		}
	} else if aggressive {
		d.Bet = &to
	}
	return d
}

func TestClonePredrawUsesLegalContextActionOrDefers(t *testing.T) {
	model := &sixmaxclone.Model{Schema: sixmaxclone.SchemaVersion, MinGroups: 20, MinConfidence: .5, MinMargin: .1,
		Trees: map[string]sixmaxclone.Tree{
			"facing":  {Nodes: []sixmaxclone.Node{{Leaf: &sixmaxclone.Leaf{Groups: 30, Probabilities: [3]float64{0, 0, 1}}}}},
			"checked": {Nodes: []sixmaxclone.Node{{Leaf: &sixmaxclone.Leaf{Groups: 30, Probabilities: [3]float64{0, 0, 1}}}}},
		}}
	bot := New(DefaultConfig())
	bot.Clone = model
	bot.State = newSixState(1)
	bot.State.Match.SmallBlind = 50
	bot.State.Hand.Cards = cards.MustParse("2c", "3d", "7h", "Js", "Kc")
	to := wire.Range{MinTo: 200, MaxTo: 200}
	if got := bot.Decide(wire.Decision{Kind: wire.DecisionWager, Check: true, Raise: &to}); got.Kind != wire.ActionRaise {
		t.Fatalf("limped BB action = %s", got.Kind)
	}
	if got := bot.Decide(wire.Decision{Kind: wire.DecisionWager, Check: true}); got.Kind != wire.ActionCheck {
		t.Fatalf("capped checked action = %s, want baseline check", got.Kind)
	}
	call := uint64(25)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call}
	baseline := New(DefaultConfig())
	baseline.State = bot.State
	if got, want := bot.Decide(d), baseline.Decide(d); got.Kind != want.Kind {
		t.Fatalf("unavailable learned raise = %s, want baseline %s", got.Kind, want.Kind)
	}
}

func TestClonePredrawUsesBigBlindAsSmallBet(t *testing.T) {
	leaf := func(action sixmaxclone.Action) sixmaxclone.Node {
		probabilities := [3]float64{}
		probabilities[action] = 1
		return sixmaxclone.Node{Leaf: &sixmaxclone.Leaf{Groups: 30, Probabilities: probabilities}}
	}
	bot := New(DefaultConfig())
	bot.Clone = &sixmaxclone.Model{Schema: sixmaxclone.SchemaVersion, MinGroups: 20, MinConfidence: .5, MinMargin: .1,
		Trees: map[string]sixmaxclone.Tree{
			"facing": {Nodes: []sixmaxclone.Node{leaf(sixmaxclone.Passive)}},
			"checked": {Nodes: []sixmaxclone.Node{
				{Feature: sixmaxclone.FeaturePotSmallBets, Threshold: 3.5, Left: 1, Right: 2},
				leaf(sixmaxclone.Aggressive), leaf(sixmaxclone.Passive),
			}},
		}}
	bot.State = newSixState(1)
	bot.State.Match.SmallBlind, bot.State.Match.BigBlind = 40, 100
	bot.State.Hand.Pot = 350
	bot.State.Hand.Cards = cards.MustParse("2c", "3d", "7h", "Js", "Kc")
	to := wire.Range{MinTo: 200, MaxTo: 200}
	if got := bot.Decide(wire.Decision{Kind: wire.DecisionWager, Check: true, Raise: &to}); got.Kind != wire.ActionRaise {
		t.Fatalf("clone action = %s, want raise from three-big-blind feature bucket", got.Kind)
	}
}

func TestClonePredrawDefersUnsafeStackAndPotStates(t *testing.T) {
	model := &sixmaxclone.Model{Schema: sixmaxclone.SchemaVersion, MinGroups: 20, MinConfidence: .5, MinMargin: .1,
		Trees: map[string]sixmaxclone.Tree{
			"facing":  {Nodes: []sixmaxclone.Node{{Leaf: &sixmaxclone.Leaf{Groups: 30, Probabilities: [3]float64{0, 0, 1}}}}},
			"checked": {Nodes: []sixmaxclone.Node{{Leaf: &sixmaxclone.Leaf{Groups: 30, Probabilities: [3]float64{0, 0, 1}}}}},
		}}
	call := uint64(100)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call, Raise: &wire.Range{MinTo: 200, MaxTo: 200}}
	for _, tc := range []struct {
		name  string
		setup func(*Bot)
	}{
		{"existing all-in", func(b *Bot) { b.State.Hand.Seats[1].AllIn = true }},
		{"existing side pot", func(b *Bot) { b.State.Hand.SidePot = true }},
		{"call reaches remaining stack", func(b *Bot) {
			b.State.Hand.Seats[3].Stack = 150
			b.State.Hand.Seats[3].Contribution = 50
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bot := New(DefaultConfig())
			bot.Clone = model
			bot.State = newSixState(3)
			bot.State.Hand.Cards = cards.MustParse("3c", "5d", "8h", "Ks", "Ac")
			tc.setup(bot)
			baseline := New(DefaultConfig())
			baseline.State = bot.State
			if got, want := bot.Decide(d), baseline.Decide(d); got.Kind != want.Kind {
				t.Fatalf("clone action = %s, want baseline %s", got.Kind, want.Kind)
			}
		})
	}
}

func TestClonePredrawDefersStackExhaustingAggressionTargets(t *testing.T) {
	model := &sixmaxclone.Model{Schema: sixmaxclone.SchemaVersion, MinGroups: 20, MinConfidence: .5, MinMargin: .1,
		Trees: map[string]sixmaxclone.Tree{
			"facing":  {Nodes: []sixmaxclone.Node{{Leaf: &sixmaxclone.Leaf{Groups: 30, Probabilities: [3]float64{0, 0, 1}}}}},
			"checked": {Nodes: []sixmaxclone.Node{{Leaf: &sixmaxclone.Leaf{Groups: 30, Probabilities: [3]float64{0, 0, 1}}}}},
		}}
	for _, tc := range []struct {
		name                                string
		facing                              bool
		stack, contribution, street, target uint64
	}{
		{"raise exactly exhausts", true, 250, 100, 50, 200},
		{"short all-in raise", true, 250, 100, 100, 250},
		{"bet exactly exhausts", false, 200, 50, 0, 150},
		{"unequal current commitment", true, 400, 200, 75, 275},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bot := New(DefaultConfig())
			bot.Clone = model
			bot.State = newSixState(3)
			bot.State.Hand.Cards = cards.MustParse("3c", "5d", "8h", "Ks", "Ac")
			hero := &bot.State.Hand.Seats[3]
			hero.Stack, hero.Contribution, hero.StreetCommit = tc.stack, tc.contribution, tc.street
			d := wire.Decision{Kind: wire.DecisionWager, Check: !tc.facing}
			if tc.facing {
				call := uint64(100)
				d.Fold, d.Call = true, &call
				d.Raise = &wire.Range{MinTo: tc.target, MaxTo: tc.target}
			} else {
				d.Bet = &wire.Range{MinTo: tc.target, MaxTo: tc.target}
			}
			baseline := New(DefaultConfig())
			baseline.State = bot.State
			if got, want := bot.Decide(d), baseline.Decide(d); got.Kind != want.Kind {
				t.Fatalf("clone action = %s, want baseline %s", got.Kind, want.Kind)
			}
		})
	}
}

func TestClonePredrawDefersCheckedActionWithNoRemainingStack(t *testing.T) {
	model := &sixmaxclone.Model{Schema: sixmaxclone.SchemaVersion, MinGroups: 20, MinConfidence: .5, MinMargin: .1,
		Trees: map[string]sixmaxclone.Tree{
			"facing":  {Nodes: []sixmaxclone.Node{{Leaf: &sixmaxclone.Leaf{Groups: 30, Probabilities: [3]float64{0, 1, 0}}}}},
			"checked": {Nodes: []sixmaxclone.Node{{Leaf: &sixmaxclone.Leaf{Groups: 30, Probabilities: [3]float64{0, 0, 1}}}}},
		}}
	bot := New(DefaultConfig())
	bot.Clone = model
	bot.State = newSixState(3)
	bot.State.Hand.Cards = cards.MustParse("3c", "5d", "8h", "Ks", "Ac")
	bot.State.Hand.Seats[3].Stack = 100
	bot.State.Hand.Seats[3].Contribution = 100
	d := wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: &wire.Range{MinTo: 100, MaxTo: 100}}
	baseline := New(DefaultConfig())
	baseline.State = bot.State
	if got, want := bot.Decide(d), baseline.Decide(d); got.Kind != want.Kind {
		t.Fatalf("clone action = %s, want baseline %s", got.Kind, want.Kind)
	}
}

func TestClonePredrawNeverFoldsExactNuts(t *testing.T) {
	leaf := &sixmaxclone.Leaf{Groups: 30, Probabilities: [3]float64{1, 0, 0}}
	model := &sixmaxclone.Model{Schema: sixmaxclone.SchemaVersion, MinGroups: 20, MinConfidence: .5,
		Trees: map[string]sixmaxclone.Tree{
			"facing":  {Nodes: []sixmaxclone.Node{{Leaf: leaf}}},
			"checked": {Nodes: []sixmaxclone.Node{{Leaf: leaf}}},
		}}
	bot := New(DefaultConfig())
	bot.Clone = model
	bot.State = newSixState(3)
	bot.State.Hand.Cards = cards.MustParse("2c", "3d", "4h", "5s", "7c")
	call := uint64(100)
	raise := wire.Range{MinTo: 200, MaxTo: 200}
	if got := bot.Decide(wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call, Raise: &raise}); got.Kind == wire.ActionFold {
		t.Fatal("learned model folded exact predraw nuts")
	}
}

func TestFrozenCloneExactNutsContexts(t *testing.T) {
	path := os.Getenv("SIXMAX_CLONE_MODEL")
	if path == "" {
		t.Skip("set SIXMAX_CLONE_MODEL")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	model, err := sixmaxclone.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	hand := cards.MustParse("2c", "3d", "4h", "5s", "7c")
	rawFolds := 0
	contexts := 0
	for position := 0; position < SeatCount; position++ {
		for aggressions := 0; aggressions < 4; aggressions++ {
			for _, facing := range []bool{false, true} {
				call := uint64(0)
				if facing {
					call = 100
				}
				features, err := sixmaxclone.BuildFeatures(sixmaxclone.Snapshot{Hand: hand, Position: position,
					ActivePlayers: 6, Aggressions: aggressions, Pot: 1000, Call: call, SmallBet: 100})
				if err != nil {
					t.Fatal(err)
				}
				if action, ok := model.Predict(features); ok && action == sixmaxclone.Fold {
					rawFolds++
				}
				bot := New(DefaultConfig())
				bot.Clone = model
				bot.State = newSixState(position)
				bot.State.Hand.Cards = hand
				bot.State.Hand.Pot = 1000
				bot.State.Hand.StreetAggressions = aggressions
				raise := wire.Range{MinTo: 200, MaxTo: 200}
				decision := wire.Decision{Kind: wire.DecisionWager, Check: !facing, Raise: &raise}
				if facing {
					decision.Fold, decision.Call = true, &call
				}
				if got := bot.Decide(decision); got.Kind == wire.ActionFold {
					t.Fatalf("guarded exact nuts folded at position=%d aggressions=%d facing=%t", position, aggressions, facing)
				}
				contexts++
			}
		}
	}
	t.Logf("frozen exact-nuts contexts=%d raw learned folds=%d", contexts, rawFolds)
}

func TestPredrawPositionAndActionState(t *testing.T) {
	bot := New(DefaultConfig())
	bot.State = newSixState(3)                                           // UTG
	bot.State.Hand.Cards = cards.MustParse("3c", "5d", "8h", "Ks", "Ac") // sound late-only two-card draw
	if got := bot.Decide(wager(true, true)).Kind; got != wire.ActionFold {
		t.Fatalf("UTG weak two-draw = %s, want fold", got)
	}
	bot.State.Hand.Hero = 5 // cutoff widens
	if got := bot.Decide(wager(true, true)).Kind; got != wire.ActionRaise {
		t.Fatalf("CO first-in = %s, want raise", got)
	}
	bot.State.Observe(wire.Event{Kind: wire.EventActed, Seat: 3, Action: wire.Raise(200), StreetCommit: 200})
	bot.State.Hand.Cards = cards.MustParse("2c", "3d", "Kh", "Qs", "Ac") // three-card draw
	if got := bot.Decide(wager(true, true)).Kind; got != wire.ActionFold {
		t.Fatalf("cold-call three-draw = %s, want fold", got)
	}
}

func TestDrawBreaksRoughMadeHandsMultiwayAndNeverSnows(t *testing.T) {
	bot := New(DefaultConfig())
	bot.State = newSixState(0)
	bot.State.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw1})
	bot.State.Hand.Cards = cards.MustParse("2c", "4d", "6h", "8s", "9c")
	bot.State.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
	a := bot.Decide(wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5})
	if a.Kind != wire.ActionDiscard || len(a.Cards) == 0 {
		t.Fatalf("rough nine vs pat multiway = %+v, want break", a)
	}
	bot.State.Hand.Cards = cards.MustParse("2c", "3d", "4h", "5s", "Kc")
	a = bot.Decide(wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5})
	if len(a.Cards) == 0 {
		t.Fatal("unmade hand stood pat (snowed)")
	}
}

func TestEveryReplyIsAuthoritativelyLegal(t *testing.T) {
	bot := New(DefaultConfig())
	bot.State = newSixState(5)
	bot.State.Hand.Cards = cards.MustParse("2c", "3d", "4h", "5s", "7c")
	cases := []wire.Decision{
		wager(false, false), wager(false, true), wager(true, false), wager(true, true),
		{Kind: wire.DecisionDraw, MaxDiscards: 2},
	}
	for _, d := range cases {
		got := bot.Decide(d)
		g, _ := json.Marshal(got)
		legal, _ := json.Marshal(wire.Legalize(d, got, bot.State.Hand.Cards))
		if string(g) != string(legal) {
			t.Errorf("decision %+v yielded illegal %s, legalized %s", d, g, legal)
		}
	}
}

func TestPostdrawValueAndDrawContinuation(t *testing.T) {
	tests := []struct {
		name   string
		street int
		hand   []cards.Card
		setup  func(*Bot)
		d      wire.Decision
		want   string
	}{
		{"value raises", Draw1, cards.MustParse("2c", "3d", "4h", "6s", "8c"), nil, wager(true, true), wire.ActionRaise},
		{"eight only calls pat", Draw1, cards.MustParse("2c", "3d", "4h", "6s", "8c"), func(b *Bot) {
			b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
		}, wager(true, true), wire.ActionCall},
		{"junk checks free", Draw1, cards.MustParse("2c", "3d", "4h", "5s", "Kc"), nil, wager(false, true), wire.ActionCheck},
		{"nine calls", Draw2, cards.MustParse("2c", "3d", "5h", "7s", "9c"), nil, wager(true, true), wire.ActionCall},
		{"one draw calls", Draw2, cards.MustParse("2c", "3d", "5h", "7s", "Kc"), nil, wager(true, true), wire.ActionCall},
		{"two draw cheap early", Draw1, cards.MustParse("2c", "3d", "7h", "Qs", "Kc"), func(b *Bot) { b.State.Hand.Pot = 500 }, wager(true, true), wire.ActionCall},
		{"two draw folds late", Draw2, cards.MustParse("2c", "3d", "7h", "Qs", "Kc"), nil, wager(true, true), wire.ActionFold},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := New(DefaultConfig())
			b.State = newSixState(0)
			b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: tc.street})
			b.State.Hand.Cards = tc.hand
			if tc.setup != nil {
				tc.setup(b)
			}
			if got := b.Decide(tc.d).Kind; got != tc.want {
				t.Fatalf("action = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRiverUsesAllLivePlayersAndPendingAction(t *testing.T) {
	newRiver := func(hand []cards.Card) *Bot {
		b := New(DefaultConfig())
		b.State = newSixState(0)
		b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw3})
		b.State.Hand.Cards = hand
		return b
	}

	seven := newRiver(cards.MustParse("2c", "3d", "4h", "5s", "7c"))
	for seat := 1; seat < SeatCount; seat++ {
		seven.State.Hand.Seats[seat].ActedOnStreet = true
	}
	if got := seven.Decide(wager(true, true)).Kind; got != wire.ActionRaise {
		t.Fatalf("closed river seven = %s", got)
	}

	nine := newRiver(cards.MustParse("2c", "3d", "5h", "7s", "9c"))
	if got := nine.Decide(wager(true, true)).Kind; got != wire.ActionFold {
		t.Fatalf("multiway river nine = %s", got)
	}
	if got := nine.Decide(wager(false, true)).Kind; got != wire.ActionCheck {
		t.Fatalf("multiway river nine checked-to = %s", got)
	}

	eight := newRiver(cards.MustParse("2c", "3d", "4h", "6s", "8c"))
	if got := eight.Decide(wager(true, true)).Kind; got != wire.ActionCall {
		t.Fatalf("multiway river eight = %s", got)
	}
	if got := eight.Decide(wager(false, true)).Kind; got != wire.ActionBet {
		t.Fatalf("river eight value bet = %s", got)
	}
}

func TestClosingRiverNutsRaiseThroughPatPressureAndCallWhenCapped(t *testing.T) {
	b := New(DefaultConfig())
	b.State = newSixState(0)
	b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw3})
	b.State.Hand.Cards = cards.MustParse("2c", "3d", "4h", "5s", "7c")
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
	for seat := 1; seat < SeatCount; seat++ {
		b.State.Hand.Seats[seat].ActedOnStreet = true
	}
	if got := b.Decide(wager(true, true)).Kind; got != wire.ActionRaise {
		t.Fatalf("nuts vs pat = %s, want raise", got)
	}
	if got := b.Decide(wager(true, false)).Kind; got != wire.ActionCall {
		t.Fatalf("capped nuts = %s, want call", got)
	}
	b.State.Hand.Seats[2].ActedOnStreet = false
	if got := b.Decide(wager(true, true)).Kind; got != wire.ActionCall {
		t.Fatalf("non-closing nuts = %s, want existing call behavior", got)
	}
}

func TestPricedDefenseOnlyAfterOwnOpen(t *testing.T) {
	setup := func(pot uint64) *Bot {
		b := New(DefaultConfig())
		b.State = newSixState(5)
		b.State.Hand.Cards = cards.MustParse("2c", "3d", "7h", "Qs", "Kc")
		b.Observe(wire.Event{Kind: wire.EventActed, Seat: 5, Action: wire.Raise(200), StreetCommit: 200})
		b.Observe(wire.Event{Kind: wire.EventActed, Seat: 0, Action: wire.Raise(300), StreetCommit: 300})
		b.State.Hand.Pot = pot
		return b
	}
	cheap := setup(500)
	if got := cheap.Decide(wager(true, true)).Kind; got != wire.ActionCall {
		t.Fatalf("own-open cheap defense = %s, want call", got)
	}
	high := setup(300)
	if got := high.Decide(wager(true, true)).Kind; got != wire.ActionFold {
		t.Fatalf("own-open expensive defense = %s, want fold", got)
	}

	cold := New(DefaultConfig())
	cold.State = newSixState(5)
	cold.State.Hand.Cards = cards.MustParse("2c", "3d", "7h", "Qs", "Kc")
	cold.Observe(wire.Event{Kind: wire.EventActed, Seat: 3, Action: wire.Raise(200), StreetCommit: 200})
	cold.Observe(wire.Event{Kind: wire.EventActed, Seat: 4, Action: wire.Raise(300), StreetCommit: 300})
	cold.State.Hand.Pot = 500
	if got := cold.Decide(wager(true, true)).Kind; got != wire.ActionFold {
		t.Fatalf("cold entry after two bets = %s, want fold", got)
	}
}

func TestValueAggressionLeadsOnlyWhenEnabled(t *testing.T) {
	makeBot := func(hand []cards.Card) *Bot {
		b := New(DefaultConfig())
		b.State = newSixState(0)
		b.State.Hand.Street = Draw2
		b.State.Hand.Cards = hand
		for s := 2; s < SeatCount; s++ {
			b.State.Hand.Seats[s].Folded = true
		}
		b.State.Hand.Seats[1].Draws[Draw2] = 1
		return b
	}
	d := wager(false, true)
	b := makeBot(cards.MustParse("2c", "3d", "4h", "6s", "9c"))
	if got := b.Decide(d).Kind; got != wire.ActionCheck {
		t.Fatalf("default nine=%s", got)
	}
	b.Config.ValueAggression = true
	if got := b.Decide(d).Kind; got != wire.ActionBet {
		t.Fatalf("enabled nine=%s", got)
	}
}

func TestStrongDrawLeadMatchesEnabledIntendedDraw(t *testing.T) {
	b := New(DefaultConfig())
	b.State = newSixState(0)
	b.State.Hand.Street = Draw1
	b.State.Hand.Cards = cards.MustParse("2c", "3d", "4h", "6s", "Tc")
	for s := 2; s < SeatCount; s++ {
		b.State.Hand.Seats[s].Folded = true
	}
	b.State.Hand.Seats[1].Draws[Draw1] = 2
	b.Config.ValueAggression = true
	if got := b.Decide(wager(false, true)).Kind; got != wire.ActionCheck {
		t.Fatalf("aggression-only patted ten led=%s", got)
	}
	b.Config.EarlyTenBreak = true
	if got := b.Decide(wager(false, true)).Kind; got != wire.ActionBet {
		t.Fatalf("both-enabled drawing ten=%s", got)
	}
}

func TestEarlyTenBreakOnlyWhenEnabled(t *testing.T) {
	b := New(DefaultConfig())
	b.State = newSixState(0)
	b.State.Hand.Street = Draw1
	b.State.Hand.Cards = cards.MustParse("2c", "3d", "4h", "6s", "Tc")
	for s := 2; s < SeatCount; s++ {
		b.State.Hand.Seats[s].Folded = true
	}
	b.State.Hand.Seats[1].Draws[Draw1] = 2
	d := wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5}
	if got := b.Decide(d); len(got.Cards) != 0 {
		t.Fatalf("default ten discard=%v", got.Cards)
	}
	b.Config.EarlyTenBreak = true
	if got := b.Decide(d); len(got.Cards) != 1 || got.Cards[0].Rank != cards.Ten {
		t.Fatalf("enabled ten discard=%v", got.Cards)
	}
}

func TestBotLifecycleDelegatesToSixSeatState(t *testing.T) {
	b := New(DefaultConfig())
	cap, timeout := uint8(4), uint64(1000)
	b.Hello(wire.Message{GameID: "27td-fl", SeatCount: 6, Betting: wire.Betting{RaiseCap: &cap}, TimeoutMs: &timeout})
	b.HandStart(wire.Message{HandNo: 42, Seat: 4})
	b.Observe(wire.Event{Kind: wire.EventHandStart, Button: 0})
	if b.State.Match.RaiseCap != 4 || b.State.Match.TimeoutMs != 1000 || b.State.Hand.No != 42 || b.State.Position(4) != Hijack {
		t.Fatalf("lifecycle state = %+v %+v", b.State.Match, b.State.Hand)
	}
	if Button.String() != "button" || b.State.Position(-1) != UnderTheGun {
		t.Fatal("position boundary behavior changed")
	}
}

func riverRangeBot(t *testing.T, hand []cards.Card, pot uint64, masses ...sixmaxrange.RankMass) (*Bot, wire.Decision) {
	t.Helper()
	b := New(DefaultConfig())
	b.State = newSixState(0)
	b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw3})
	b.State.Hand.Cards, b.State.Hand.Pot = hand, pot
	for seat := 2; seat < SeatCount; seat++ {
		b.Observe(wire.Event{Kind: wire.EventActed, Seat: seat, Action: wire.Fold()})
	}
	b.State.Hand.Seats[1].ActedOnStreet = true
	b.State.Hand.Seats[1].Draws[Draw3] = 1
	b.Ranges = &sixmaxrange.Model{Schema: sixmaxrange.SchemaVersion, Source: "test", Cells: []sixmaxrange.Cell{{
		Level: "action", Key: sixmaxrange.None, EffectiveGroups: 20,
		Distribution: sixmaxrange.Distribution{Ranks: masses},
	}}}
	call := uint64(100)
	return b, wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call}
}

func TestRangeOverlayOnlyChangesSafeClosingRiverCalls(t *testing.T) {
	hand := cards.MustParse("2c", "3d", "4h", "5s", "Kc")
	value := deuce.Eval(hand)
	b, d := riverRangeBot(t, hand, 1000, sixmaxrange.RankMass{Value: value - 1, Mass: 1})
	if got := b.Decide(d).Kind; got != wire.ActionCall {
		t.Fatalf("supported closing +EV = %s", got)
	}
	b.State.Hand.Seats[1].ActedOnStreet = false
	if got := b.Decide(d).Kind; got != wire.ActionFold {
		t.Fatalf("pending action overlay = %s", got)
	}
	b.State.Hand.Seats[1].ActedOnStreet = true
	b.State.Hand.Seats[1].AllIn = true
	if got := b.Decide(d).Kind; got != wire.ActionFold {
		t.Fatalf("all-in overlay = %s", got)
	}
	b.State.Hand.Seats[1].AllIn = false
	b.State.Hand.SidePot = true
	if got := b.Decide(d).Kind; got != wire.ActionFold {
		t.Fatalf("side-pot overlay=%s", got)
	}
	b.State.Hand.SidePot = false
	b.State.Hand.Seats[1].Draws[Draw3] = -1
	if got := b.Decide(d).Kind; got != wire.ActionFold {
		t.Fatalf("unknown-final overlay=%s", got)
	}
	b.State.Hand.Seats[1].Draws[Draw3] = 1
	b.Ranges = &sixmaxrange.Model{Schema: sixmaxrange.SchemaVersion, Source: "unsupported"}
	if got := b.Decide(d).Kind; got != wire.ActionFold {
		t.Fatalf("unsupported overlay = %s", got)
	}
}

func TestRangeOverlayUsesActualPotAndCall(t *testing.T) {
	hand := cards.MustParse("2c", "3d", "4h", "5s", "Kc")
	value := deuce.Eval(hand)
	masses := []sixmaxrange.RankMass{{Value: value - 1, Mass: .2}, {Value: value + 1, Mass: .8}}
	b, d := riverRangeBot(t, hand, 1000, masses...)
	if got := b.Decide(d).Kind; got != wire.ActionCall {
		t.Fatalf("large-pot call = %s", got)
	}
	b.State.Hand.Pot = 100
	if got := b.Decide(d).Kind; got != wire.ActionFold {
		t.Fatalf("small-pot call = %s", got)
	}
}

func TestRangeOverlayDefersCallsThatPutHeroAllIn(t *testing.T) {
	hand := cards.MustParse("2c", "3d", "4h", "5s", "Kc")
	value := deuce.Eval(hand)
	for _, tc := range []struct {
		name                      string
		stack, contribution, call uint64
	}{{"short stack", 50, 0, 50}, {"exact remaining", 150, 50, 100}, {"exhausted", 100, 100, 1}, {"invalid stack", 0, 0, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			b, d := riverRangeBot(t, hand, 1000, sixmaxrange.RankMass{Value: value - 1, Mass: 1})
			b.State.Hand.Seats[0].Stack = tc.stack
			b.State.Hand.Seats[0].Contribution = tc.contribution
			d.Call = &tc.call
			if got := b.Decide(d).Kind; got != wire.ActionFold {
				t.Fatalf("all-in creating call=%s", got)
			}
		})
	}
}

func TestRangeOverlayDefersRealisticFiveOpponentPrefixWithUnsupportedCallers(t *testing.T) {
	hand := cards.MustParse("2c", "3d", "4h", "5s", "Kc")
	value := deuce.Eval(hand)
	b := New(DefaultConfig())
	b.State = newSixState(0)
	b.State.Hand.Street = Draw3
	b.State.Hand.Cards = hand
	b.State.Hand.Pot = 1000
	for seat := 1; seat < SeatCount; seat++ {
		b.State.Hand.Seats[seat].Draws[Draw2] = 1
		b.State.Hand.Seats[seat].Draws[Draw3] = 1
		b.State.Hand.Seats[seat].ActedOnStreet = true
	}
	b.State.Hand.Actions = append(b.State.Hand.Actions, ActionRecord{Street: Draw3, Seat: 1, Action: wire.Bet(200)})
	for seat := 2; seat < SeatCount; seat++ {
		b.State.Hand.Actions = append(b.State.Hand.Actions, ActionRecord{Street: Draw3, Seat: seat, Action: wire.Call()})
	}
	b.Ranges = &sixmaxrange.Model{Schema: 1, Source: "narrow", Cells: []sixmaxrange.Cell{{Level: "action", Key: sixmaxrange.Aggressive, EffectiveGroups: 20, Distribution: sixmaxrange.Distribution{Ranks: []sixmaxrange.RankMass{{Value: value - 1, Mass: 1}}}}}}
	call := uint64(100)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call}
	baseline := wire.Legalize(d, b.propose(d), hand)
	if _, ok := b.rangeCall(d, baseline); ok {
		t.Fatal("partial five-opponent support activated overlay")
	}
}

func TestRangeOverlayPreservesBaselineRaise(t *testing.T) {
	hand := cards.MustParse("2c", "3d", "4h", "5s", "7c")
	value := deuce.Eval(hand)
	b, d := riverRangeBot(t, hand, 100, sixmaxrange.RankMass{Value: value + 1, Mass: 1})
	r := wire.Range{MinTo: 200, MaxTo: 200}
	d.Raise = &r
	if got := b.Decide(d).Kind; got != wire.ActionRaise {
		t.Fatalf("range suppressed raise: %s", got)
	}
}

func BenchmarkClosingRiverRangeSupportedHeadsUp(b *testing.B) {
	path := os.Getenv("SIXMAX_RANGE_MODEL")
	if path == "" {
		b.Skip("set SIXMAX_RANGE_MODEL")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	model, err := sixmaxrange.Decode(raw)
	if err != nil {
		b.Fatal(err)
	}
	hand := cards.MustParse("2c", "3d", "4h", "5s", "Kc")
	bot := New(DefaultConfig())
	bot.State = newSixState(0)
	bot.State.Hand.Street = Draw3
	bot.State.Hand.Cards = hand
	bot.State.Hand.Pot = 1000
	for seat := 2; seat < SeatCount; seat++ {
		bot.State.Hand.Seats[seat].Folded = true
	}
	bot.State.Hand.Seats[1].Draws[Draw2] = 1
	bot.State.Hand.Seats[1].Draws[Draw3] = 1
	bot.State.Hand.Seats[1].ActedOnStreet = true
	bot.State.Hand.Actions = []ActionRecord{{Street: Draw3, Seat: 1, Action: wire.Bet(200), Commit: 200}}
	bot.Ranges = model
	call := uint64(100)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call}
	baseline := wire.Legalize(d, bot.propose(d), hand)
	if _, ok := bot.rangeCall(d, baseline); !ok {
		b.Fatal("production model does not support benchmark prefix")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bot.Decide(d)
	}
}

func BenchmarkDecideSixMax(b *testing.B) {
	bot := New(DefaultConfig())
	bot.State = newSixState(5)
	bot.State.Hand.Cards = cards.MustParse("2c", "3d", "4h", "8s", "Kc")
	d := wager(true, true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bot.Decide(d)
	}
}
