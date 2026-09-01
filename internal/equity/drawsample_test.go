package equity

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/policy"
	"github.com/nuttakit/2-7-bot/internal/table"
)

// One DrawSamples pass, aggregated per (class, context, candidate).
type tally struct {
	wins  map[sampleKey][]float64
	count map[sampleKey][]int
}

type sampleKey struct {
	class   handclass.ID
	context int
}

func collect(t *testing.T, deals int, seed int64) *tally {
	t.Helper()
	out := &tally{wins: map[sampleKey][]float64{}, count: map[sampleKey][]int{}}
	DrawSamples(nil, deals, seed, func(s DrawSample) {
		key := sampleKey{s.Class, policy.DrawContext(s.Street, s.Read)}
		if out.wins[key] == nil {
			out.wins[key] = make([]float64, policy.MaxDrawCandidates)
			out.count[key] = make([]int, policy.MaxDrawCandidates)
		}
		for i, outcome := range s.Outcomes {
			out.wins[key][i] += outcome
			out.count[key][i]++
		}
	})
	return out
}

// The generator's whole reproducibility story, as for Showdown.
func TestDrawSamplesAreDeterministic(t *testing.T) {
	first, second := collect(t, 500, 7), collect(t, 500, 7)
	for key, wins := range first.wins {
		other, ok := second.wins[key]
		if !ok {
			t.Fatalf("key %v missing on the second pass", key)
		}
		for i := range wins {
			if wins[i] != other[i] {
				t.Fatalf("key %v candidate %d: %v vs %v", key, i, wins[i], other[i])
			}
		}
	}
}

// Every emitted sample must be internally consistent: outcomes sized to the
// class's candidate list, streets in the draw range, and the read shaped the
// way position dictates — no read only before the button's first draw,
// staleness only for the big blind.
func TestDrawSamplesAreWellFormed(t *testing.T) {
	seen := 0
	DrawSamples(nil, 500, 3, func(s DrawSample) {
		seen++
		want := len(policy.DrawCandidates(handclass.Representative(s.Class)))
		if len(s.Outcomes) != want {
			t.Fatalf("class %d: %d outcomes, want %d", s.Class, len(s.Outcomes), want)
		}
		if s.Street < table.Draw1 || s.Street > table.Draw3 {
			t.Fatalf("street %d out of range", s.Street)
		}
		if !s.Read.Known && s.Street != table.Draw1 {
			t.Fatalf("no read on street %d, only Draw1 can be blind", s.Street)
		}
		if s.Read.Known && s.Read.StreetsAgo != 0 && s.Read.StreetsAgo != 1 {
			t.Fatalf("streetsAgo = %d, want 0 or 1", s.Read.StreetsAgo)
		}
		for _, outcome := range s.Outcomes {
			if outcome != 0 && outcome != 0.5 && outcome != 1 {
				t.Fatalf("outcome %v, want 0, 0.5 or 1", outcome)
			}
		}
	})
	// Three decisions per deal, hero on both sides across deals.
	if want := 500 * 3; seen != want {
		t.Fatalf("saw %d samples over 500 deals, want %d", seen, want)
	}
}

// The measurement itself, on the one decision where the answer is beyond
// sampling noise: holding the nuts, standing pat must beat every break.
func TestDrawSamplesPreferStandingOnTheNuts(t *testing.T) {
	agg := collect(t, 30000, 1)
	nuts := handclass.Of(cards.MustParse("7c", "5d", "4h", "3s", "2c"))

	checked := 0
	for key, wins := range agg.wins {
		if key.class != nuts {
			continue
		}
		counts := agg.count[key]
		if counts[0] < 30 {
			continue
		}
		pat := wins[0] / float64(counts[0])
		for i := 1; i < len(counts); i++ {
			if counts[i] == 0 {
				continue
			}
			if broke := wins[i] / float64(counts[i]); broke >= pat {
				t.Errorf("context %d: breaking the nuts (cand %d, %.3f) beat standing (%.3f)",
					key.context, i, broke, pat)
			}
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("the nut class was never sampled with enough weight to check")
	}
}
