package sixmaxsim

import (
	"reflect"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestNewPostsBlindsDealsAndOpensUTG(t *testing.T) {
	g, err := New(Config{Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if g.Actor != 3 || g.Street != Predraw || g.Wagers != 1 {
		t.Fatalf("actor/street/wagers = %d/%d/%d", g.Actor, g.Street, g.Wagers)
	}
	if g.Committed != [Seats]uint64{0, 50, 100} || g.Total != [Seats]uint64{0, 50, 100} {
		t.Fatalf("committed=%v total=%v", g.Committed, g.Total)
	}
	wantKinds := []string{wire.EventHandStart, wire.EventPost, wire.EventPost, wire.EventStreetStart}
	for i, want := range wantKinds {
		if g.Events[i].Kind != want {
			t.Fatalf("event %d = %q, want %q", i, g.Events[i].Kind, want)
		}
	}
	if g.Events[1].Seat != 1 || g.Events[1].PostKind != "small-blind" || g.Events[2].Seat != 2 || g.Events[2].PostKind != "big-blind" {
		t.Fatalf("blind events = %#v %#v", g.Events[1], g.Events[2])
	}
	for seat := 0; seat < Seats; seat++ {
		if len(g.Hands[seat]) != 5 {
			t.Fatalf("seat %d has %d cards", seat, len(g.Hands[seat]))
		}
	}
}

func TestFixedLimitRaiseCapAndRoundTransition(t *testing.T) {
	g := mustGame(t, Config{Seed: 11})
	apply(t, g, wire.Raise(200)) // UTG, wager two.
	apply(t, g, wire.Raise(300))
	apply(t, g, wire.Raise(400)) // cap reached.
	seat, legal, ok := g.Legal()
	if !ok || seat != 0 || legal.Raise != nil || legal.Call == nil || *legal.Call != 400 {
		t.Fatalf("capped legal seat=%d decision=%+v", seat, legal)
	}
	for g.Street == Predraw {
		_, d, _ := g.Legal()
		if d.Call != nil {
			apply(t, g, wire.Call())
		} else {
			apply(t, g, wire.Check())
		}
	}
	if g.Street != Draw1 || g.Actor != 1 {
		t.Fatalf("after predraw street=%d actor=%d", g.Street, g.Actor)
	}
	if _, d, ok := g.Legal(); !ok || d.Kind != wire.DecisionDraw || d.MaxDiscards != 5 {
		t.Fatalf("draw legal = %+v, ok=%v", d, ok)
	}
}

func TestFoldoutPaysLastPlayerAndBalances(t *testing.T) {
	g := mustGame(t, Config{Seed: 13})
	for !g.Done() {
		seat, d, _ := g.Legal()
		if seat == 2 {
			apply(t, g, wire.Check())
			continue
		}
		if !d.Fold {
			t.Fatalf("seat %d cannot fold: %+v", seat, d)
		}
		apply(t, g, wire.Fold())
	}
	nets, ok := g.Payoffs()
	if !ok || nets != [Seats]int64{0, -50, 50, 0, 0, 0} {
		t.Fatalf("nets=%v ok=%v", nets, ok)
	}
	assertZeroSum(t, nets)
	if hasEvent(g.Events, wire.EventShowdownShow) {
		t.Fatal("foldout emitted showdown")
	}
}

func TestFoldoutRefundsUncalledRaiseBeforeSettlement(t *testing.T) {
	g := mustGame(t, Config{Seed: 15})
	apply(t, g, wire.Raise(200))
	for !g.Done() {
		apply(t, g, wire.Fold())
	}
	if g.Total[3] != 100 || g.Committed[3] != 100 {
		t.Fatalf("uncalled raise not refunded: total=%v commit=%v", g.Total, g.Committed)
	}
	nets, _ := g.Payoffs()
	if nets != [Seats]int64{0, -50, -100, 150, 0, 0} {
		t.Fatalf("nets after refund=%v", nets)
	}
	lastPot := g.Events[len(g.Events)-2]
	if lastPot.Kind != wire.EventPotAwarded || lastPot.Winners[0][1] != 250 {
		t.Fatalf("pot award after refund=%+v", lastPot)
	}
}

func TestShowdownTieSplitsPotAndBalances(t *testing.T) {
	g := mustGame(t, Config{Seed: 17})
	g.Hands[0] = cards.MustParse("7c", "5d", "4h", "3s", "2c")
	g.Hands[2] = cards.MustParse("7d", "5h", "4s", "3c", "2d")
	for !g.Done() {
		seat, d, _ := g.Legal()
		switch {
		case d.Kind == wire.DecisionDraw:
			apply(t, g, wire.Discard(nil))
		case g.Street == Predraw && seat != 0 && seat != 2:
			apply(t, g, wire.Fold())
		case d.Call != nil:
			apply(t, g, wire.Call())
		default:
			apply(t, g, wire.Check())
		}
	}
	nets, _ := g.Payoffs()
	if nets != [Seats]int64{25, -50, 25, 0, 0, 0} {
		t.Fatalf("tie nets=%v", nets)
	}
	award := g.Events[len(g.Events)-2]
	if got := award.Winners; len(got) != 2 || got[0][0] != 0 || got[1][0] != 2 {
		t.Fatalf("pot winners not sorted by seat: %v", got)
	}
	assertZeroSum(t, nets)
}

func TestDrawConservesCardsAndExcludesCurrentDiscardsFromReshuffle(t *testing.T) {
	g := mustGame(t, Config{Seed: 19})
	// Keep seats 0 and 2, putting four folded hands into the muck.
	for g.Street == Predraw {
		seat, d, _ := g.Legal()
		switch {
		case seat != 0 && seat != 2:
			apply(t, g, wire.Fold())
		case d.Call != nil:
			apply(t, g, wire.Call())
		default:
			apply(t, g, wire.Check())
		}
	}
	for !g.Done() {
		_, d, _ := g.Legal()
		if d.Kind == wire.DecisionDraw {
			discarded := append([]cards.Card(nil), g.Hands[g.Actor]...)
			apply(t, g, wire.Discard(discarded))
			last := g.Events[len(g.Events)-1]
			for _, drawn := range last.Drawn {
				if cards.Contains(discarded, drawn) {
					t.Fatalf("seat immediately redrew own discard %s", drawn)
				}
			}
		} else {
			apply(t, g, wire.Check())
		}
		assertCardConservation(t, g)
	}
}

func TestPrivateEventsAreRedactedForOtherSeats(t *testing.T) {
	g := mustGame(t, Config{Seed: 23})
	for _, event := range g.EventsFor(0) {
		if event.Kind == wire.EventDealHole && event.Seat != 0 && (len(event.Cards) != 0 || event.Count != 5) {
			t.Fatalf("opponent hole event leaked: %+v", event)
		}
	}
	for g.Street == Predraw {
		_, d, _ := g.Legal()
		if d.Call != nil {
			apply(t, g, wire.Call())
		} else {
			apply(t, g, wire.Check())
		}
	}
	owner := g.Actor
	discard := append([]cards.Card(nil), g.Hands[owner][:2]...)
	apply(t, g, wire.Discard(discard))
	event := g.Events[len(g.Events)-1]
	if got := EventFor(event, (owner+1)%Seats); len(got.Discarded) != 0 || len(got.Drawn) != 0 || got.Count != 2 {
		t.Fatalf("opponent draw event leaked: %+v", got)
	}
	if got := EventFor(event, owner); len(got.Discarded) != 2 || len(got.Drawn) != 2 {
		t.Fatalf("owner draw event redacted: %+v", got)
	}
}

func TestDrawOrderVisitsEveryLiveSeatOnceWithButtonLast(t *testing.T) {
	g := mustGame(t, Config{Seed: 27})
	for g.Street == Predraw {
		_, d, _ := g.Legal()
		if d.Call != nil {
			apply(t, g, wire.Call())
		} else {
			apply(t, g, wire.Check())
		}
	}
	want := []int{1, 2, 3, 4, 5, 0}
	for i, seat := range want {
		if g.Actor != seat {
			t.Fatalf("draw %d actor=%d want=%d", i, g.Actor, seat)
		}
		apply(t, g, wire.Discard(nil))
	}
	if g.Street != Draw1 || g.Actor != 1 {
		t.Fatalf("betting did not open after button draw: street=%d actor=%d", g.Street, g.Actor)
	}
}

func TestSeedAndClonePreserveExactChanceState(t *testing.T) {
	a := mustGame(t, Config{Seed: 29})
	b := mustGame(t, Config{Seed: 29})
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same seed produced different state")
	}
	clone := a.Clone()
	apply(t, a, wire.Call())
	apply(t, clone, wire.Call())
	if !reflect.DeepEqual(a, clone) {
		t.Fatal("clone diverged under identical action")
	}
	clone.Hands[0][0] = cards.Card{}
	clone.Events[4].Cards[0] = cards.Card{}
	if reflect.DeepEqual(a.Hands[0], clone.Hands[0]) || reflect.DeepEqual(a.Events[4], clone.Events[4]) {
		t.Fatal("clone shares nested slices")
	}
}

func TestEngineRNGSnapshotIsFrozen(t *testing.T) {
	rng := newRNG(0, 0)
	want := [...]uint64{
		11_091_344_671_253_066_420,
		8_173_996_640_537_286_706,
		16_113_819_434_696_063_216,
		4_438_403_619_926_855_730,
	}
	for i, expected := range want {
		if got := rng.next(); got != expected {
			t.Fatalf("rng value %d=%d want=%d", i, got, expected)
		}
	}
}

func TestPinnedEngineSeed4242CallerOracle(t *testing.T) {
	// Captured from the pinned Rust engine with six builtin:caller seats,
	// seeded dealing, seed 4242, one hand. This freezes deck direction as well
	// as the full passive action/draw/showdown transition sequence.
	g := mustGame(t, Config{Seed: 4242})
	wantHands := [Seats][]cards.Card{
		cards.MustParse("2c", "Ts", "9d", "7d", "5d"),
		cards.MustParse("Td", "5s", "Kh", "Jc", "5h"),
		cards.MustParse("Th", "9h", "2h", "Qd", "Tc"),
		cards.MustParse("7h", "9s", "6s", "6c", "Js"),
		cards.MustParse("Ah", "3h", "Jd", "3s", "5c"),
		cards.MustParse("Qc", "Ks", "As", "4h", "2d"),
	}
	if !reflect.DeepEqual(g.Hands, wantHands) {
		for seat := 0; seat < Seats; seat++ {
			t.Logf("seat %d got=%v want=%v", seat, cards.Strings(g.Hands[seat]), cards.Strings(wantHands[seat]))
		}
		t.Fatal("initial deal differs from pinned engine")
	}
	if err := g.Continue(func(int, uint64) (Player, error) { return callerPlayer{}, nil }); err != nil {
		t.Fatal(err)
	}
	nets, _ := g.Payoffs()
	if nets != [Seats]int64{500, -100, -100, -100, -100, -100} {
		t.Fatalf("caller oracle nets=%v", nets)
	}
	if len(g.Events) != 63 {
		t.Fatalf("caller oracle emitted %d events, want 63", len(g.Events))
	}
}

func TestForcedHandValidationAndRemoval(t *testing.T) {
	forced := cards.MustParse("7c", "5d", "4h", "3s", "2c")
	g := mustGame(t, Config{Seed: 31, Forced: &ForcedHand{Seat: 4, Cards: forced}})
	if !reflect.DeepEqual(g.Hands[4], forced) {
		t.Fatalf("forced hand=%v", cards.Strings(g.Hands[4]))
	}
	seen := map[cards.Card]bool{}
	for seat := 0; seat < Seats; seat++ {
		for _, card := range g.Hands[seat] {
			if seen[card] {
				t.Fatalf("card dealt twice: %s", card)
			}
			seen[card] = true
		}
	}
	bad := []Config{
		{Forced: &ForcedHand{Seat: -1, Cards: forced}},
		{Forced: &ForcedHand{Seat: 0, Cards: forced[:4]}},
		{Forced: &ForcedHand{Seat: 0, Cards: append(forced[:4], forced[0])}},
	}
	for _, config := range bad {
		if _, err := New(config); err == nil {
			t.Fatalf("accepted bad forced hand: %+v", config.Forced)
		}
	}
}

func TestContinueReplaysArbitraryPrefixWithPrivateRedaction(t *testing.T) {
	g := mustGame(t, Config{Seed: 37})
	apply(t, g, wire.Call())
	leaf := g.Clone()
	var players [Seats]*recordingPlayer
	err := leaf.Continue(func(seat int, seed uint64) (Player, error) {
		if seed != 37 {
			t.Fatalf("factory seed=%d", seed)
		}
		player := &recordingPlayer{seat: seat}
		players[seat] = player
		return player, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !leaf.Done() {
		t.Fatal("continuation did not settle")
	}
	for seat, player := range players {
		if !player.hello || !player.handStart {
			t.Fatalf("seat %d missed framing callbacks", seat)
		}
		if len(player.events) == 0 || player.events[0].Kind != wire.EventHandStart {
			t.Fatalf("seat %d missed observed hand-start", seat)
		}
		if !hasEvent(player.events, wire.EventActed) {
			t.Fatalf("seat %d missed applied prefix action", seat)
		}
		for _, event := range player.events {
			if event.Kind == wire.EventDealHole && event.Seat != seat && len(event.Cards) != 0 {
				t.Fatalf("seat %d saw seat %d cards", seat, event.Seat)
			}
		}
	}
}

type recordingPlayer struct {
	seat      int
	hello     bool
	handStart bool
	events    []wire.Event
}

type callerPlayer struct{}

func (callerPlayer) Hello(wire.Message)     {}
func (callerPlayer) HandStart(wire.Message) {}
func (callerPlayer) Observe(wire.Event)     {}
func (callerPlayer) Decide(decision wire.Decision) wire.Action {
	if decision.Kind == wire.DecisionDraw {
		return wire.Discard(nil)
	}
	if decision.Call != nil {
		return wire.Call()
	}
	return wire.Check()
}

func (p *recordingPlayer) Hello(wire.Message)     { p.hello = true }
func (p *recordingPlayer) HandStart(wire.Message) { p.handStart = true }
func (p *recordingPlayer) Observe(event wire.Event) {
	p.events = append(p.events, event)
}
func (p *recordingPlayer) Decide(decision wire.Decision) wire.Action {
	if decision.Kind == wire.DecisionDraw {
		return wire.Discard(nil)
	}
	if decision.Fold {
		return wire.Fold()
	}
	return wire.Check()
}

func mustGame(t *testing.T, config Config) *Game {
	t.Helper()
	g, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func apply(t *testing.T, g *Game, action wire.Action) {
	t.Helper()
	if err := g.Apply(action); err != nil {
		t.Fatalf("seat %d street %d apply %+v: %v", g.Actor, g.Street, action, err)
	}
}

func hasEvent(events []wire.Event, kind string) bool {
	for _, event := range events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

func assertZeroSum(t *testing.T, nets [Seats]int64) {
	t.Helper()
	var sum int64
	for _, net := range nets {
		sum += net
	}
	if sum != 0 {
		t.Fatalf("nets do not sum to zero: %v", nets)
	}
}

func assertCardConservation(t *testing.T, g *Game) {
	t.Helper()
	seen := map[cards.Card]string{}
	add := func(card cards.Card, where string) {
		if prior, ok := seen[card]; ok {
			t.Fatalf("card %s in %s and %s", card, prior, where)
		}
		seen[card] = where
	}
	for seat := 0; seat < Seats; seat++ {
		for _, card := range g.Hands[seat] {
			add(card, "hand")
		}
	}
	for _, card := range g.deck {
		add(card, "deck")
	}
	for _, card := range g.muck {
		add(card, "muck")
	}
	if len(seen) != 52 {
		t.Fatalf("conserved %d cards, want 52", len(seen))
	}
}
