// Package policy is the bot's strategy: a predraw range chart, a draw rule
// and a betting rule, each a fixed sequence of comparisons rather than a
// search.
//
// h2's one change over h1 is the chart's Open and Defend columns, which are
// generated from equity rollouts (cmd/chartgen) instead of hand-tuned —
// the 2026-08-30 benchmarks measured h1's tight chart as its dominant leak,
// ~12 BB/100 to every hosted rival. The shapes, keeps, draw rule and
// betting rule are h1's, unchanged.
//
// Nothing here mixes and nothing here bluffs, still. Being deterministic
// makes the bot fully exploitable by anything that models it — a measured
// ~3 BB/100 against the tuned blueprint — and closing that is a later
// generation's change, not this one's. See draw.go on the missing snow.
package policy

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/handclass"
)

// Move is what to do with a hand before the first draw.
type Move uint8

// The three predraw moves.
const (
	Fold Move = iota
	Call
	Raise
)

// Shape is the drawing category a hand plays as, named by how many cards it
// keeps. Category priority keeps the most cards — pat over one-card over
// two-card over three-card — so a rough pat nine is never reclassified as the
// strong draw its low cards would also make.
type Shape uint8

// The shapes, weakest first.
const (
	// Junk is outside every opening range.
	Junk Shape = iota
	// ThreeCardDraw keeps two cards, a deuce and one card seven or under.
	ThreeCardDraw
	// TwoCardDraw keeps three distinct cards eight or under.
	TwoCardDraw
	// OneCardDraw keeps four.
	OneCardDraw
	// Pat is a made nine or better as dealt.
	Pat
)

var shapeName = [...]string{"junk", "three-card draw", "two-card draw", "one-card draw", "pat"}

func (s Shape) String() string {
	if int(s) >= len(shapeName) {
		return "?"
	}
	return shapeName[s]
}

// Chart is the predraw reading of one hand.
type Chart struct {
	Shape Shape
	// Keep is the ranks worth holding, one card per rank. Five ranks is a
	// stand pat.
	Keep []cards.Rank
	// Open is the button's first-in move, heads-up: raise or fold. Limping
	// is not in the range.
	Open Move
	// Defend is the big blind's move facing an open.
	Defend Move
}

// Classify reads a five-card hand against the range chart.
//
// The shape ladder and keep lists below are structural: they encode three
// facts about the game (docs/game/rules.md) — eight-or-better is the useful
// drawing objective, a nine low beats any straight so straight-shaped draws
// are traps, and the ace is always high so A-5-4-3-2 is a bad hand rather
// than a wheel. They are unchanged from h1, and the draw rule depends on
// them.
//
// The Open and Defend columns are h2's change: they come from the generated
// chartTable (cmd/chartgen), which cuts an equity-rollout ranking of all
// predraw hand classes at frequencies derived from pot odds and a
// fixed-point iteration of each range against the other. Which hands to
// play is a measured number; only *how* to defend — three-bet or flat —
// keeps h1's structural rule, because the table stores a continue bit, not
// a sizing.
func Classify(hand []cards.Card) Chart {
	distinct := cards.DistinctRanks(hand)
	low7 := atMost(distinct, cards.Seven)
	low8 := atMost(distinct, cards.Eight)
	flush := cards.SameSuit(hand)

	// Pat: five distinct ranks, nine low or better, no straight and no
	// flush. The ace is always high, so A-5-4-3-2 is not a straight — but
	// it is not a nine low either, so it never reaches here.
	pat := len(distinct) == 5 && !flush && !consecutive(distinct) && distinct[4] <= cards.Nine
	// A smooth nine — nine-six or better — is strong enough to three-bet.
	smoothPat := pat && distinct[3] <= cards.Six

	// One-card draws: four cards seven or under that are not an
	// open-ender, or failing that four cards eight or under that are not
	// a four-straight at all. 2-3-4-5 is only a one-ender — a six makes
	// the straight, a seven makes the nuts — so an open-ender is four in
	// a row starting at a three or higher.
	var keepFour []cards.Rank
	switch {
	case pat:
	case len(low7) >= 4 && !(consecutive(low7[:4]) && low7[0] >= cards.Three):
		keepFour = low7[:4]
	case len(low8) >= 4 && !consecutive(low8[:4]):
		keepFour = low8[:4]
	}
	// The three-betting draw: a clean eight-low draw with no straight risk.
	strongDraw := !pat && len(low8) >= 4 && !consecutive(low8[:4])

	// Two-card draws: three distinct cards eight or under, excluding the
	// pure middle straights 4-5-6, 5-6-7 and 6-7-8.
	var keepThree []cards.Rank
	if len(low8) >= 3 && !(consecutive(low8[:3]) && low8[0] >= cards.Four) {
		keepThree = low8[:3]
	}

	// Three-card draws: a deuce with one card seven or under. Without a
	// deuce you cannot draw to 7-5-4-3-2 at all.
	var keepTwo []cards.Rank
	if distinct[0] == cards.Two && len(distinct) >= 2 && distinct[1] <= cards.Seven {
		keepTwo = distinct[:2]
	}

	open, defends := tableMoves(hand)
	switch {
	case pat:
		return Chart{Shape: Pat, Keep: distinct, Open: open, Defend: defendWith(defends, smoothPat)}
	case keepFour != nil:
		return Chart{Shape: OneCardDraw, Keep: keepFour, Open: open, Defend: defendWith(defends, strongDraw)}
	case keepThree != nil:
		return Chart{Shape: TwoCardDraw, Keep: keepThree, Open: open,
			Defend: defendWith(defends, keepThree[0] == cards.Two)}
	case keepTwo != nil:
		return Chart{Shape: ThreeCardDraw, Keep: keepTwo, Open: open, Defend: defendWith(defends, false)}
	default:
		return Chart{Shape: Junk, Keep: salvageKeep(distinct), Open: open, Defend: defendWith(defends, false)}
	}
}

// tableMoves reads the generated chart: whether this hand opens the button,
// and whether it continues from the big blind.
func tableMoves(hand []cards.Card) (open Move, defends bool) {
	entry := chartTable[handclass.Of(hand)]
	open = Fold
	if entry&chartOpenBit != 0 {
		open = Raise
	}
	return open, entry&chartDefendBit != 0
}

// The chartTable bit layout, shared with cmd/chartgen.
const (
	chartOpenBit   = 1
	chartDefendBit = 2
)

// defendWith folds outside the defending range, three-bets the structural
// top of it — h1's rule, unchanged — and flats the rest.
func defendWith(defends, strong bool) Move {
	switch {
	case !defends:
		return Fold
	case strong:
		return Raise
	default:
		return Call
	}
}

// DrawingKeep is the best set of ranks to keep when we are drawing rather
// than standing pat — the same category ladder as Classify with the pat
// branch removed, since the decision to break has already been made by the
// caller.
//
// This is the cheap version of candidate generation: one keep per hand rather
// than one candidate per discard count scored by a draw-quality metric.
// Widening it to several candidates and choosing between them by exact
// one-draw enumeration is the obvious next step.
func DrawingKeep(hand []cards.Card) []cards.Rank {
	chart := Classify(hand)
	if chart.Shape != Pat {
		return chart.Keep
	}
	return salvageKeep(cards.DistinctRanks(hand))
}

// salvageKeep is the fallback for a hand in no opening category, and for a
// made hand being broken: the lowest distinct ranks nine or under, at most
// four of them, refusing to draw at a straight.
//
// The two straight rules are the same ones Classify applies, deliberately.
// Without them a hand the chart rejected as a middle-straight trap could be
// handed straight back to the draw as exactly that shape.
func salvageKeep(distinct []cards.Rank) []cards.Rank {
	keep := atMost(distinct, cards.Nine)
	if len(keep) > 4 {
		keep = keep[:4]
	}
	if len(keep) == 4 && consecutive(keep) && keep[0] >= cards.Three {
		keep = keep[:3]
	}
	if len(keep) == 3 && consecutive(keep) && keep[0] >= cards.Four {
		keep = keep[:2]
	}
	return keep
}

// Discards turns a keep list into the concrete cards to throw away:
// everything beyond one copy of each kept rank. Pairs are dead weight in
// lowball, so the second copy of a rank always goes.
func Discards(hand []cards.Card, keep []cards.Rank) []cards.Card {
	wanted := make(map[cards.Rank]int, len(keep))
	for _, rank := range keep {
		wanted[rank]++
	}
	discards := make([]cards.Card, 0, len(hand))
	for _, card := range cards.SortedByRank(hand) {
		if wanted[card.Rank] > 0 {
			wanted[card.Rank]--
			continue
		}
		discards = append(discards, card)
	}
	return discards
}

// atMost returns the prefix of an ascending rank list at or below a ceiling.
func atMost(ascending []cards.Rank, ceiling cards.Rank) []cards.Rank {
	for i, rank := range ascending {
		if rank > ceiling {
			return ascending[:i]
		}
	}
	return ascending
}

// consecutive reports whether ranks form an unbroken run. A single rank is
// not a run.
func consecutive(ranks []cards.Rank) bool {
	for i := 1; i < len(ranks); i++ {
		if ranks[i] != ranks[i-1]+1 {
			return false
		}
	}
	return len(ranks) > 1
}
