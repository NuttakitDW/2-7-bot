package garnet

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/beryl"
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestRuntimeGateExcludesAndFreeBigBlindChecks(t *testing.T) {
	bot := testBot(t, Percentages{}, Strategy{Passive: 0, Raise: 1, Visits: 10})
	startBot(bot, 3, cards.MustParse("7c", "5d", "4h", "3s", "2c"))
	decision := facingDecision(100, 200)
	if got := bot.Decide(decision); got.Kind != wire.ActionFold {
		t.Fatalf("excluded UTG action=%+v want fold", got)
	}

	bot = testBot(t, Percentages{}, Strategy{Passive: 0, Raise: 1, Visits: 10})
	startBot(bot, 2, cards.MustParse("7c", "5d", "4h", "3s", "2c"))
	if got := bot.Decide(freeDecision(200)); got.Kind != wire.ActionCheck {
		t.Fatalf("excluded free BB action=%+v want check", got)
	}
}

func TestRuntimeAdmittedEntryUsesPolicyAndIsNotRegated(t *testing.T) {
	var percentages Percentages
	percentages[UnderTheGun][Unopened] = 100
	bot := testBot(t, percentages, Strategy{Passive: 0, Raise: 1, Visits: 20})
	startBot(bot, 3, cards.MustParse("Kc", "Qd", "Jh", "Ts", "2c"))
	decision := facingDecision(100, 200)
	if got := bot.Decide(decision); got.Kind != wire.ActionRaise || got.To != 200 {
		t.Fatalf("admitted policy action=%+v want raise 200", got)
	}
	bot.Observe(wire.Event{Kind: wire.EventActed, Seat: 3, Action: wire.Raise(200), StreetCommit: 200})
	bot.Observe(wire.Event{Kind: wire.EventActed, Seat: 4, Action: wire.Raise(300), StreetCommit: 300})
	// SingleOpen is configured at zero percent, but admission was frozen by
	// the first decision and the learned policy still acts.
	if got := bot.Decide(facingDecision(100, 400)); got.Kind != wire.ActionRaise || got.To != 400 {
		t.Fatalf("admitted hand was re-gated: %+v", got)
	}
}

func TestRuntimePassiveAndUntrainedPoliciesStayInsideAdmittedRange(t *testing.T) {
	var percentages Percentages
	percentages[Cutoff][Unopened] = 100
	bot := testBot(t, percentages, Strategy{Passive: 1, Raise: 0, Visits: 10})
	startBot(bot, 5, cards.MustParse("7c", "5d", "4h", "3s", "2c"))
	if got := bot.Decide(facingDecision(100, 200)); got.Kind != wire.ActionCall {
		t.Fatalf("passive strategy=%+v want call", got)
	}

	for position := Position(0); position < Positions; position++ {
		for context := Context(0); context < Contexts; context++ {
			bot.policy.Backoff[position][context] = nil
		}
	}
	bot.reference.HandStart(wire.Message{Type: wire.MsgHandStart, Seat: 5, HandNo: 1})
	bot.Observe(wire.Event{Kind: wire.EventHandStart, Button: 0})
	bot.Observe(wire.Event{Kind: wire.EventDealHole, Seat: 5, Cards: cards.MustParse("7c", "5d", "4h", "3s", "2c"), Count: 5})
	if got := bot.Decide(facingDecision(100, 200)); got.Kind != wire.ActionCall || bot.Untrained != 1 {
		t.Fatalf("untrained action=%+v count=%d want admitted call/count1", got, bot.Untrained)
	}
}

func TestRuntimeLookupCountersExcludeGateRejections(t *testing.T) {
	var percentages Percentages
	percentages[UnderTheGun][Unopened] = 100
	bot := testBot(t, percentages, Strategy{Passive: 1, Visits: 10})
	hand := cards.MustParse("7c", "5d", "4h", "3s", "2c")
	decision := facingDecision(100, 200)
	view := beryl.PredrawView{Hand: hand, Position: beryl.UnderTheGun, Decision: decision, Active: 0x3f, Wagers: 1}
	bucket, ok := bot.bucketer.Bucket(hand)
	if !ok {
		t.Fatal("failed to bucket valid hand")
	}
	bot.policy.Infosets = map[string]Strategy{KeyFor(view, bucket): {Passive: 1, Visits: 20}}
	if _, ok := bot.predraw(view); !ok || bot.Exact != 1 || bot.Backoffs != 0 || bot.Untrained != 0 {
		t.Fatalf("exact lookup counters exact=%d backoff=%d untrained=%d", bot.Exact, bot.Backoffs, bot.Untrained)
	}

	excluded := testBot(t, Percentages{}, Strategy{Raise: 1, Visits: 10})
	view.Hand = cards.MustParse("Kc", "Qd", "Jh", "Ts", "2c")
	if _, ok := excluded.predraw(view); !ok || excluded.Exact != 0 || excluded.Backoffs != 0 || excluded.Untrained != 0 {
		t.Fatalf("gate rejection changed lookup counters exact=%d backoff=%d untrained=%d", excluded.Exact, excluded.Backoffs, excluded.Untrained)
	}
}

func TestRuntimeCapForcedCallDoesNotCountAsPolicyLookup(t *testing.T) {
	var percentages Percentages
	percentages[UnderTheGun][MultipleOpen] = 100
	bot := testBot(t, percentages, Strategy{Raise: 1, Visits: 10})
	call := uint64(100)
	view := beryl.PredrawView{
		Hand: cards.MustParse("7c", "5d", "4h", "3s", "2c"), Position: beryl.UnderTheGun,
		Decision: wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call},
		Active:   0x3f, Wagers: 4, Raises: 3, EntryDecided: true, Admitted: true,
	}
	choice, ok := bot.predraw(view)
	if !ok || choice.Action.Kind != wire.ActionCall || bot.ForcedPassive != 1 {
		t.Fatalf("forced choice=%+v ok=%v count=%d", choice, ok, bot.ForcedPassive)
	}
	if bot.Exact != 0 || bot.Backoffs != 0 || bot.Untrained != 0 {
		t.Fatalf("forced call counted as lookup exact=%d backoff=%d untrained=%d", bot.Exact, bot.Backoffs, bot.Untrained)
	}
}

func TestRuntimePolicyMixIsDeterministicPerHandAndSeat(t *testing.T) {
	var percentages Percentages
	percentages[Hijack][Unopened] = 100
	strategy := Strategy{Passive: 0.5, Raise: 0.5, Visits: 100}
	a := testBot(t, percentages, strategy)
	b := testBot(t, percentages, strategy)
	hand := cards.MustParse("8c", "6d", "4h", "3s", "2c")
	startBot(a, 4, hand)
	startBot(b, 4, hand)
	if got, want := a.Decide(facingDecision(100, 200)), b.Decide(facingDecision(100, 200)); !reflect.DeepEqual(got, want) {
		t.Fatalf("same policy seed/hand/seat diverged: %+v vs %+v", got, want)
	}
}

func TestRuntimeKeyIsInvariantAcrossAllButtonRotations(t *testing.T) {
	hand := cards.MustParse("8c", "6d", "4h", "3s", "2c")
	var want string
	for button := 0; button < 6; button++ {
		bot := testBot(t, Percentages{}, Strategy{Passive: 1, Visits: 1})
		hero := (button + 5) % 6 // Cutoff, after a button-relative Hijack open.
		opener := (button + 4) % 6
		bot.Hello(sixMaxHello())
		bot.HandStart(wire.Message{Type: wire.MsgHandStart, HandNo: 7, Seat: hero})
		bot.Observe(wire.Event{Kind: wire.EventHandStart, Button: button})
		bot.Observe(wire.Event{Kind: wire.EventPost, Seat: (button + 1) % 6, PostKind: "small-blind", Amount: 50})
		bot.Observe(wire.Event{Kind: wire.EventPost, Seat: (button + 2) % 6, PostKind: "big-blind", Amount: 100})
		bot.Observe(wire.Event{Kind: wire.EventStreetStart, Street: 0, Label: "predraw"})
		bot.Observe(wire.Event{Kind: wire.EventDealHole, Seat: hero, Cards: hand, Count: 5})
		bot.Observe(wire.Event{Kind: wire.EventActed, Street: 0, Seat: opener, Action: wire.Raise(200), StreetCommit: 200})

		var got string
		original := bot.reference.PredrawOverride
		bot.reference.PredrawOverride = func(view beryl.PredrawView) (beryl.PredrawChoice, bool) {
			bucket, ok := bot.bucketer.Bucket(view.Hand)
			if !ok {
				t.Fatal("failed to bucket valid hand")
			}
			got = KeyFor(view, bucket)
			return original(view)
		}
		_ = bot.Decide(facingDecision(100, 200))
		if button == 0 {
			want = got
		} else if got != want {
			t.Fatalf("button %d key=%s want canonical %s", button, got, want)
		}
	}
}

func TestTrackedPlaceholderAssetsFailClosed(t *testing.T) {
	if !bytes.Equal(bytes.TrimSpace(embeddedRanking), []byte("{}")) || !bytes.Equal(bytes.TrimSpace(embeddedPolicy), []byte("{}")) {
		t.Skip("generated Garnet asset overlay is active")
	}
	if _, err := New(); err == nil {
		t.Fatal("tracked placeholder assets unexpectedly constructed a bot")
	}
}

func testBot(t *testing.T, percentages Percentages, strategy Strategy) *Bot {
	t.Helper()
	ranking := testRanking(t, func(id handclass.ID) float64 { return -float64(id) })
	gate, err := NewGate(ranking, percentages)
	if err != nil {
		t.Fatal(err)
	}
	bucketer, err := NewBucketer(ranking, 16)
	if err != nil {
		t.Fatal(err)
	}
	policy := &Policy{Version: PolicyVersion, Percentages: percentages, BucketCount: 16, Seed: 91, Iterations: 100}
	for position := Position(0); position < Positions; position++ {
		for context := Context(0); context < Contexts; context++ {
			policy.Backoff[position][context] = make([]Strategy, 16)
			for bucket := range policy.Backoff[position][context] {
				policy.Backoff[position][context][bucket] = strategy
			}
		}
	}
	reference, err := beryl.NewDeterministic(cfr.Heuristic{}, 13)
	if err != nil {
		t.Fatal(err)
	}
	return newBot(reference, gate, bucketer, policy)
}

func startBot(bot *Bot, seat int, hand []cards.Card) {
	bot.Hello(sixMaxHello())
	bot.HandStart(wire.Message{Type: wire.MsgHandStart, HandNo: 0, Seat: seat})
	bot.Observe(wire.Event{Kind: wire.EventHandStart, Button: 0})
	bot.Observe(wire.Event{Kind: wire.EventPost, Seat: 1, PostKind: "small-blind", Amount: 50})
	bot.Observe(wire.Event{Kind: wire.EventPost, Seat: 2, PostKind: "big-blind", Amount: 100})
	bot.Observe(wire.Event{Kind: wire.EventStreetStart, Street: 0, Label: "predraw"})
	bot.Observe(wire.Event{Kind: wire.EventDealHole, Seat: seat, Cards: hand, Count: 5})
}

func sixMaxHello() wire.Message {
	cap := uint8(4)
	return wire.Message{Type: wire.MsgHello, GameID: "27td-fl", SeatCount: 6, StartingStack: 10_000,
		Stakes: wire.Stakes{Kind: "blinds", SmallBlind: 50, BigBlind: 100}, Betting: wire.Betting{Kind: "fixed-limit", RaiseCap: &cap}}
}

func facingDecision(call, raiseTo uint64) wire.Decision {
	return wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call,
		Raise: &wire.Range{MinTo: raiseTo, MaxTo: raiseTo}}
}

func freeDecision(raiseTo uint64) wire.Decision {
	return wire.Decision{Kind: wire.DecisionWager, Check: true,
		Raise: &wire.Range{MinTo: raiseTo, MaxTo: raiseTo}}
}
