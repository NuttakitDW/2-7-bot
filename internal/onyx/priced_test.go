package onyx

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/policy"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestPricedOpeningOnlyAddsSelectedButtonHands(t *testing.T) {
	oldProfile, oldTable := predrawProfile, pricedOpening
	t.Cleanup(func() { predrawProfile, pricedOpening = oldProfile, oldTable })
	predrawProfile = "baseline"
	b := setup("5c", "8d", "9h", "Ks", "5h")
	predrawProfile = "priced"
	pricedOpening = [handclass.Num]uint8{}
	b.Table.Hand.Wagers = 1
	id := handclass.Of(b.Table.Hand.Cards)
	if policy.Classify(b.Table.Hand.Cards).Open != policy.Fold {
		t.Fatal("test hand is already an open")
	}
	call := uint64(50)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call, Raise: &wire.Range{MinTo: 200, MaxTo: 200}}
	for _, tc := range []struct {
		entry uint8
		want  string
	}{{0, wire.ActionFold}, {1, wire.ActionCall}, {2, wire.ActionRaise}} {
		pricedOpening[id] = tc.entry
		if got := b.propose(d, .5); got.Kind != tc.want {
			t.Fatalf("entry %d got %s want %s", tc.entry, got.Kind, tc.want)
		}
	}
	// With an empty table every existing opening hand still raises.
	pricedOpening = [handclass.Num]uint8{}
	for i := handclass.ID(0); i < handclass.Num; i++ {
		if handclass.Weight(i) == 0 {
			continue
		}
		hand := handclass.Representative(i)
		if policy.Classify(hand).Open != policy.Raise {
			continue
		}
		b.Table.Hand.Cards = hand
		if b.propose(d, .5).Kind != wire.ActionRaise {
			t.Fatal("changed an existing open")
		}
	}
	// A selected button addition must not alter BB defense.
	b.Table.Hand.Cards = cards.MustParse("6c", "9d", "Th", "Ks", "Ac")
	b.Table.Hand.Seat, b.Table.Hand.Wagers = 1, 2
	pricedOpening[handclass.Of(b.Table.Hand.Cards)] = 2
	if b.propose(d, .5).Kind != wire.ActionFold {
		t.Fatal("priced button range changed BB defense")
	}
}

func TestPricedProfileRejectsMissingOrInvalidTable(t *testing.T) {
	oldProfile, oldTable := predrawProfile, pricedOpening
	t.Cleanup(func() { predrawProfile, pricedOpening = oldProfile, oldTable })
	predrawProfile = "priced"
	pricedOpening = [handclass.Num]uint8{}
	if _, err := New(); err == nil {
		t.Fatal("accepted missing priced table")
	}
	id := handclass.Of(cards.MustParse("5c", "8d", "9h", "Ks", "5h"))
	pricedOpening[id] = 3
	if _, err := New(); err == nil {
		t.Fatal("accepted invalid action")
	}
	pricedOpening[id] = 1
	if _, err := New(); err != nil {
		t.Fatal(err)
	}
}
