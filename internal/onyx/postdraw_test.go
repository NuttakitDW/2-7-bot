package onyx

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/table"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestSnowProfilesRespectFrequency(t *testing.T) {
	original := snowProfile
	t.Cleanup(func() { snowProfile = original })
	for _, tc := range []struct {
		profile string
		mix     float64
		want    bool
	}{
		{"baseline", .5, true},
		{"balanced", .2, true},
		{"balanced", .5, false},
		{"none", 0, false},
	} {
		snowProfile = tc.profile
		b := setup("2c", "3d", "Th", "Ks", "Ac")
		b.Table.Hand.Street = table.Draw3
		b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 1})
		b.propose(wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5}, tc.mix)
		if b.snow != tc.want {
			t.Errorf("%s mix %.2f: snow=%v want %v", tc.profile, tc.mix, b.snow, tc.want)
		}
	}
}

func TestAdaptiveDrawBreaksNineEarlyAgainstOneCardDraw(t *testing.T) {
	original := drawProfile
	drawProfile = "adaptive"
	t.Cleanup(func() { drawProfile = original })
	b := setup("2c", "3d", "4h", "7s", "9c")
	b.Table.Hand.Street = table.Draw1
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 1})
	a := b.Decide(wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5})
	if len(a.Cards) != 1 || a.Cards[0].String() != "9c" {
		t.Fatalf("did not improve nine: %+v", a)
	}
	// Keep the same made hand against a much weaker two-card draw.
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 2})
	a = b.Decide(wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5})
	if len(a.Cards) != 0 {
		t.Fatalf("broke nine against weak draw: %+v", a)
	}
}
