package zircon

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestEmbeddedH5BMetadataAndDefaultProfile(t *testing.T) {
	model := cloneModel()
	if model.Source != "swit2-swit3-grouped-cart" || model.MinConfidence != .65 {
		t.Fatalf("metadata=%q/%v", model.Source, model.MinConfidence)
	}
	if New().profile != Generation2 || NewBaseline().profile != Baseline {
		t.Fatal("constructor profiles changed")
	}
}

func cloneSpot(hand ...string) (*Bot, wire.Decision) {
	b := NewGeneration2()
	b.State = testState(3)
	b.State.Hand.Cards = cards.MustParse(hand...)
	b.State.Hand.Pot = 600
	b.State.Hand.StreetAggressions = 1
	b.State.Hand.LastAggressor = 2
	b.State.Hand.Seats[3].StreetCommit = 100
	call := uint64(100)
	return b, wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call, Raise: &wire.Range{MinTo: 300, MaxTo: 300}}
}

func TestH5BFeatureIntegrationOverridesCallToRaise(t *testing.T) {
	b, decision := cloneSpot("2c", "3d", "4h", "8s", "Kc")
	baseline := NewBaseline()
	baseline.State = b.State
	if got := baseline.Decide(decision).Kind; got != wire.ActionCall {
		t.Fatalf("baseline=%s", got)
	}
	if got := b.Decide(decision).Kind; got != wire.ActionRaise {
		t.Fatalf("generation2=%s", got)
	}
	stats := b.OverrideStats()
	if stats.Applied != 1 || stats.Matrix[1][2] != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestH5BDefersOutsideGuardedFacingRaise(t *testing.T) {
	setups := []struct {
		name   string
		mutate func(*Bot, *wire.Decision)
	}{
		{"no aggression", func(b *Bot, _ *wire.Decision) { b.State.Hand.StreetAggressions = 0 }},
		{"zero call", func(_ *Bot, d *wire.Decision) { call := uint64(0); d.Call = &call }},
		{"postdraw", func(b *Bot, _ *wire.Decision) { b.State.Hand.Street = Draw1 }},
		{"all-in present", func(b *Bot, _ *wire.Decision) { b.State.Hand.Seats[1].AllIn = true }},
		{"side pot", func(b *Bot, _ *wire.Decision) { b.State.Hand.SidePot = true }},
		{"short call", func(b *Bot, d *wire.Decision) {
			b.State.Hand.Seats[3].Stack = 200
			b.State.Hand.Seats[3].Contribution = 100
			call := uint64(100)
			d.Call = &call
		}},
		{"stack-exhausting raise", func(b *Bot, _ *wire.Decision) {
			b.State.Hand.Seats[3].Stack = 300
			b.State.Hand.Seats[3].Contribution = 100
		}},
	}
	for _, test := range setups {
		t.Run(test.name, func(t *testing.T) {
			b, decision := cloneSpot("2c", "3d", "4h", "8s", "Kc")
			test.mutate(b, &decision)
			baseline := NewBaseline()
			baseline.State = b.State
			if got, want := b.Decide(decision).Kind, baseline.Decide(decision).Kind; got != want {
				t.Fatalf("got=%s want baseline=%s", got, want)
			}
		})
	}
}

func TestH5BNeverOverridesMadeSevenOrEight(t *testing.T) {
	for _, hand := range [][]string{{"2c", "3d", "4h", "5s", "7c"}, {"2c", "3d", "4h", "6s", "8c"}} {
		b, decision := cloneSpot(hand...)
		baseline := NewBaseline()
		baseline.State = b.State
		if deuce.Categorize(b.State.Hand.Cards) < deuce.Eight {
			t.Fatal("bad fixture")
		}
		if got, want := b.Decide(decision).Kind, baseline.Decide(decision).Kind; got != want {
			t.Fatalf("hand=%v got=%s want=%s", hand, got, want)
		}
	}
}
