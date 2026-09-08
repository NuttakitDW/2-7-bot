package onyx

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/policy"
)

// predrawProfile is embedded at build time so each experimental artifact has
// one fixed strategy: -X github.com/nuttakit/2-7-bot/internal/onyx.predrawProfile=limp
// raise/limp profiles vary entry for previously folded button hands.
// smooth-limp/value-limp also limp existing marginal opens and check weak
// BB options; baseline reproduces the original opening policy.
// No experimental range is promoted until it improves measured performance.
var predrawProfile = "baseline"

// predrawDefense isolates BB continuation from the opening experiment.
// wide removes the extra filter on two useful ranks within the chart's
// defending range; pressure also three-bets clean two-card draws against
// a single button raise. Post-draw decisions are unaffected by either setting.
var predrawDefense = "original"

func additionalOpen(hand []cards.Card) bool {
	if predrawProfile == "baseline" {
		return false
	}
	keep := policy.DrawingKeep(hand)
	if len(keep) >= 3 {
		return true
	}
	if len(keep) < 2 {
		return false
	}
	if predrawProfile == "wide-raise" || predrawProfile == "wide-limp" || predrawProfile == "value-limp" {
		return true // any two distinct ranks nine or lower
	}
	// Smooth three-card draws retain two useful low ranks. Paired copies
	// do not count as extra kept cards, and straight traps are broken by
	// DrawingKeep before this test.
	return keep[0] <= cards.Five && keep[1] <= cards.Eight
}
