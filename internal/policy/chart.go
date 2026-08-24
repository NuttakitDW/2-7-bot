// Package policy is the bot's strategy: a predraw range chart, a draw rule
// and a betting rule, each a fixed sequence of comparisons rather than a
// search.
//
// Nothing here mixes and nothing here bluffs. Both omissions are deliberate:
// h1's purpose is to be a correct, readable floor — an actual hand ranking and
// an actual draw rule — not a contender. Being deterministic makes it fully
// exploitable by anything that models it, which is the price paid for being
// legible. See draw.go on the missing snow.
package policy

import "github.com/nuttakit/2-7-bot/internal/cards"

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
// The thresholds are ported from this repo's previous bot
// (`git show 2131e7c:strategy.go`), which is where they were first written
// down. They are **untuned heuristics, not computed equities** — nothing in
// this repo derives them, and no measurement says they are right. What they
// encode is three structural facts about the game (docs/game/rules.md):
// eight-or-better is the useful drawing objective, a nine low beats any
// straight so straight-shaped draws are traps, and the ace is always high so
// A-5-4-3-2 is a bad hand rather than a wheel.
//
// Note what the code actually does, which is wider than any published human
// guidance: the two-card branch opens *any* three distinct cards eight or
// under except the runs 4-5-6, 5-6-7 and 6-7-8, and does not require a deuce.
// Replacing these rows with numbers from exhaustive one-draw enumeration is
// the cheapest real improvement available here.
//
// The old version precomputed all 7,462 rank classes into a map. That is not
// needed: this runs in a few hundred nanoseconds against a budget measured in
// hundreds of microseconds, and reading it beats reading a table builder.
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

	switch {
	case pat:
		return Chart{Shape: Pat, Keep: distinct, Open: Raise, Defend: defendWith(smoothPat)}
	case keepFour != nil:
		return Chart{Shape: OneCardDraw, Keep: keepFour, Open: Raise, Defend: defendWith(strongDraw)}
	case keepThree != nil:
		return Chart{Shape: TwoCardDraw, Keep: keepThree, Open: Raise,
			Defend: defendWith(keepThree[0] == cards.Two)}
	case keepTwo != nil:
		return Chart{Shape: ThreeCardDraw, Keep: keepTwo, Open: Raise, Defend: Call}
	default:
		return Chart{Shape: Junk, Keep: salvageKeep(distinct), Open: Fold, Defend: Fold}
	}
}

// defendWith three-bets the top of each category and calls with the rest.
func defendWith(strong bool) Move {
	if strong {
		return Raise
	}
	return Call
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
