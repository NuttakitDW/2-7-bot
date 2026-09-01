package equity

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/handclass"
)

const samples = 4000

func equityOf(t *testing.T, hand string, opp Accept) float64 {
	t.Helper()
	hero := make([]cards.Card, 0, 5)
	for i := 0; i+1 < len(hand); i += 2 {
		card, err := cards.ParseCard(hand[i : i+2])
		if err != nil {
			t.Fatalf("hand %q: %v", hand, err)
		}
		hero = append(hero, card)
	}
	return Showdown(hero, opp, samples, 1)
}

// The seed is the whole reproducibility story for the generated chart, so
// identical inputs must give identical outputs.
func TestShowdownIsDeterministic(t *testing.T) {
	first := equityOf(t, "9c8d7h5s2c", nil)
	second := equityOf(t, "9c8d7h5s2c", nil)
	if first != second {
		t.Errorf("same seed, different equity: %v vs %v", first, second)
	}
}

// The ranking is what the chart cuts, so the obvious orderings must hold
// with a distance no sampling noise at this volume can flip.
func TestShowdownOrdersHands(t *testing.T) {
	nuts := equityOf(t, "7c5d4h3s2c", nil)    // the best hand there is
	nine := equityOf(t, "9c8d7h5s2c", nil)    // a rough made nine
	oneCard := equityOf(t, "2c3d4h7sKc", nil) // a strong one-card draw
	junk := equityOf(t, "AcKdQhJs9c", nil)    // nothing at all

	if !(nuts > nine && nine > junk) {
		t.Errorf("ordering broken: nuts %.3f, nine %.3f, junk %.3f", nuts, nine, junk)
	}
	if !(oneCard > junk) {
		t.Errorf("a one-card draw (%.3f) must beat junk (%.3f)", oneCard, junk)
	}
	if nuts < 0.85 {
		t.Errorf("the nuts vs a random hand = %.3f, want near-certain", nuts)
	}
	if junk > 0.45 {
		t.Errorf("junk vs a random hand = %.3f, want clearly below half", junk)
	}
}

// Restricting the villain to the hero's own class must pull equity to the
// neighborhood of a coin flip — the mirror match — which exercises the
// rejection sampler end to end.
func TestShowdownAgainstARange(t *testing.T) {
	nutClass := handclass.Of(cards.MustParse("7c", "5d", "4h", "3s", "2c"))
	mirror := equityOf(t, "7c5d4h3s2c", func(id handclass.ID) bool { return id == nutClass })
	if mirror < 0.45 || mirror > 0.65 {
		t.Errorf("nuts vs the nut class = %.3f, want a near coin flip", mirror)
	}

	uniform := equityOf(t, "7c5d4h3s2c", nil)
	if mirror >= uniform {
		t.Errorf("equity vs the nut range (%.3f) must be below vs uniform (%.3f)", mirror, uniform)
	}
}
