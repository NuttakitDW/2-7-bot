package onyx

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/policy"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestOpenAdditionalPlayableDraws(t *testing.T) {
	original := predrawProfile
	predrawProfile = "raise"
	t.Cleanup(func() { predrawProfile = original })
	for _, hand := range [][]string{
		{"5c", "8d", "9h", "Ks", "5h"},
		{"3c", "4d", "9h", "Js", "Ac"},
		{"4c", "5d", "6h", "Qs", "Kc"},
	} {
		b := setup(hand...)
		b.Table.Hand.Wagers = 1
		call := uint64(25)
		a := b.Decide(wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call,
			Raise: &wire.Range{MinTo: 100, MaxTo: 100}})
		if a.Kind == wire.ActionFold {
			t.Errorf("folded playable opening draw %v", hand)
		}
	}
}

func TestPredrawOpeningRangeKeepsExistingOpens(t *testing.T) {
	original := predrawProfile
	t.Cleanup(func() { predrawProfile = original })
	for _, profile := range []string{"baseline", "raise", "limp", "wide-raise", "wide-limp", "smooth-limp", "value-limp"} {
		t.Run(profile, func(t *testing.T) {
			predrawProfile = profile
			call := uint64(25)
			d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call,
				Raise: &wire.Range{MinTo: 100, MaxTo: 100}}
			b, _ := New()
			b.Hello(wire.Message{SeatCount: 2})
			var total, oldOpen, newOpen int
			for id := handclass.ID(0); id < handclass.Num; id++ {
				weight := handclass.Weight(id)
				if weight == 0 {
					continue
				}
				hand := handclass.Representative(id)
				b.HandStart(wire.Message{Seat: 0})
				b.Table.Hand.Cards = hand
				b.Table.Hand.Wagers = 1
				a := b.Decide(d)
				total += weight
				if a.Kind != wire.ActionFold {
					newOpen += weight
				}
				c := policy.Classify(hand)
				if c.Open == policy.Raise {
					oldOpen += weight
					want := wire.ActionRaise
					if (profile == "smooth-limp" || profile == "value-limp") && c.Shape < policy.TwoCardDraw {
						want = wire.ActionCall
					}
					if a.Kind != want {
						t.Fatalf("existing open %v: got %s want %s", hand, a.Kind, want)
					}
				}
			}
			if total != 2598960 || profile != "baseline" && newOpen <= oldOpen || profile == "baseline" && newOpen != oldOpen {
				t.Fatalf("range failed to widen: total=%d old=%d new=%d", total, oldOpen, newOpen)
			}
			t.Logf("exact opening frequency: old=%.3f%% new=%.3f%%", 100*float64(oldOpen)/float64(total), 100*float64(newOpen)/float64(total))
		})
	}
}

func TestPredrawStillFoldsNoUsefulLowCards(t *testing.T) {
	b := setup("Tc", "Jd", "Qh", "Ks", "Ac")
	b.Table.Hand.Wagers = 1
	call := uint64(25)
	a := b.Decide(wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call,
		Raise: &wire.Range{MinTo: 100, MaxTo: 100}})
	if a.Kind != wire.ActionFold {
		t.Fatalf("opened no useful low cards: %+v", a)
	}
	// A free big-blind option must check, even with a hand outside the range.
	b.Table.Hand.Seat = 1
	a = b.Decide(wire.Decision{Kind: wire.DecisionWager, Check: true,
		Raise: &wire.Range{MinTo: 100, MaxTo: 100}})
	if a.Kind != wire.ActionCheck {
		t.Fatalf("lost free big-blind option: %+v", a)
	}
}

func TestPredrawProfilesDistinguishPriceAndRange(t *testing.T) {
	original := predrawProfile
	originalDefense := predrawDefense
	predrawDefense = "original"
	t.Cleanup(func() { predrawProfile, predrawDefense = original, originalDefense })
	for _, tc := range []struct {
		profile         string
		marginal, rough string
	}{
		{"baseline", wire.ActionFold, wire.ActionFold},
		{"raise", wire.ActionRaise, wire.ActionFold},
		{"limp", wire.ActionCall, wire.ActionFold},
		{"wide-raise", wire.ActionRaise, wire.ActionRaise},
		{"wide-limp", wire.ActionCall, wire.ActionCall},
		{"smooth-limp", wire.ActionCall, wire.ActionFold},
		{"value-limp", wire.ActionCall, wire.ActionCall},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			predrawProfile = tc.profile
			call := uint64(25)
			d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call,
				Raise: &wire.Range{MinTo: 100, MaxTo: 100}}
			for _, hand := range []struct {
				cards []string
				want  string
			}{
				{[]string{"5c", "8d", "9h", "Ks", "5h"}, tc.marginal},
				{[]string{"6c", "9d", "Th", "Ks", "Ac"}, tc.rough},
			} {
				b := setup(hand.cards...)
				b.Table.Hand.Wagers = 1
				if got := b.Decide(d); got.Kind != hand.want {
					t.Errorf("%v: got %s want %s", hand.cards, got.Kind, hand.want)
				}
			}
			// Even a wide opening profile must not loosen BB defense.
			b := setup("6c", "9d", "Th", "Ks", "Ac")
			b.Table.Hand.Seat = 1
			b.Table.Hand.Wagers = 2
			if got := b.Decide(d); got.Kind != wire.ActionFold {
				t.Errorf("profile changed BB defense: %s", got.Kind)
			}
		})
	}
	predrawProfile = "typo"
	if _, err := New(); err == nil {
		t.Fatal("accepted unknown build profile")
	}
}

func TestExpandedDefenseAndPressure(t *testing.T) {
	original := predrawDefense
	t.Cleanup(func() { predrawDefense = original })
	for _, tc := range []struct {
		profile string
		hand    []string
		want    string
	}{
		{"original", []string{"6c", "9d", "Th", "Ks", "Ac"}, wire.ActionFold},
		{"wide", []string{"6c", "9d", "Th", "Ks", "Ac"}, wire.ActionCall},
		{"pressure", []string{"6c", "9d", "Th", "Ks", "Ac"}, wire.ActionCall},
		{"wide", []string{"9c", "Td", "Jh", "Ks", "Ac"}, wire.ActionFold},
		{"pressure", []string{"3c", "4d", "7h", "Ks", "Ac"}, wire.ActionRaise},
		{"pressure", []string{"4c", "5d", "6h", "Ks", "Ac"}, wire.ActionCall},
	} {
		predrawDefense = tc.profile
		b := setup(tc.hand...)
		b.Table.Hand.Seat = 1
		b.Table.Hand.Wagers = 2
		call := uint64(50)
		d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call,
			Raise: &wire.Range{MinTo: 150, MaxTo: 150}}
		if got := b.Decide(d); got.Kind != tc.want {
			t.Errorf("%s %v: got %s want %s", tc.profile, tc.hand, got.Kind, tc.want)
		}
	}
	predrawDefense = "typo"
	if _, err := New(); err == nil {
		t.Fatal("accepted unknown defense profile")
	}
}
