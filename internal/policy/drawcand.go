package policy

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
)

// MaxDrawCandidates bounds every candidate list, which is what lets the
// generated draw table store a candidate index in a fixed width.
const MaxDrawCandidates = 6

// DrawCandidates lists the keeps worth pricing for one hand, in a fixed
// order: the stand pat first, then the structural keep DrawingKeep already
// chooses, then the alternates that rule cannot express — the same keep one
// card shorter, and the untrimmed low keeps that accept the straight risk
// the structural trims refuse.
//
// The list and its order are deterministic in the hand alone. The generated
// draw table stores indices into it, so cmd/drawgen and the runtime must
// both build it here — a table entry can then never point at a keep the
// runtime cannot reconstruct.
//
// Every candidate after the stand pat keeps one card per rank; Discards
// throws pair copies for them, and throws nothing for the stand pat, which
// lists every card's rank including duplicates.
func DrawCandidates(hand []cards.Card) [][]cards.Rank {
	pat := make([]cards.Rank, 0, len(hand))
	for _, card := range cards.SortedByRank(hand) {
		pat = append(pat, card.Rank)
	}
	out := [][]cards.Rank{pat}

	add := func(keep []cards.Rank) {
		if len(keep) == 0 || len(out) >= MaxDrawCandidates {
			return
		}
		for _, have := range out {
			if equalRanks(have, keep) {
				return
			}
		}
		out = append(out, keep)
	}

	structural := DrawingKeep(hand)
	if len(structural) == 0 {
		// Nothing nine or under to build on. The empty keep — draw five,
		// exactly what the structural rule already plays here — is a real
		// candidate for these hands, not a degenerate one.
		out = append(out, []cards.Rank{})
	}
	add(structural)
	if len(structural) >= 3 {
		add(structural[:len(structural)-1])
	}

	// The untrimmed keeps: the lowest ranks under the drawing ceilings with
	// no straight-shape trimming, at most four. Where the structural rule
	// trimmed an open-ender or a middle straight, these are the candidates
	// that draw at it anyway; elsewhere they collapse into duplicates.
	distinct := cards.DistinctRanks(hand)
	add(capAt(atMost(distinct, cards.Eight), 4))
	add(capAt(atMost(distinct, cards.Nine), 4))

	return out
}

// capAt returns at most n leading ranks.
func capAt(ranks []cards.Rank, n int) []cards.Rank {
	if len(ranks) > n {
		return ranks[:n]
	}
	return ranks
}

func equalRanks(a, b []cards.Rank) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
