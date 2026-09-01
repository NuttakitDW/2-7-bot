package policy

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/table"
)

// The structural draw rule: h1's own, untuned. Since h3 it is the fallback
// behind the generated draw table (draw_table.go), deciding only the cells
// cmd/drawgen saw too rarely to measure — about 4.5% of real decisions.
//
// One table, read against the opponent's most recent draw count, giving the
// worst made hand we will stand pat on:
//
//   - no read yet — hold a nine, break a ten;
//   - opponent drew two or more — hold a ten;
//   - opponent drew one or stood pat — hold an eight, break a nine.
//
// The reasoning behind every row is the same: the fewer cards an opponent
// drew, the more likely they are already made, and the better our hand has to
// be to stand on it. The nine-low row is the only decision here that is even
// close, and it is exactly the one worth replacing with an equity computed by
// one-draw enumeration rather than believed.
//
// The counts are free information. `draw-result` redacts `discarded` and
// `drawn` for other seats but `count` survives, it arrives before the betting,
// and it is published on every draw — so in triple draw this is *the* read and
// it costs nothing to use.
func standPatFloor(read Read) deuce.Category {
	if !read.Known {
		// No read yet — heads-up this is the big blind before the button
		// has drawn. Hold a nine, break a ten.
		return deuce.Nine
	}
	switch {
	case read.Count >= 2:
		// They are two cards away at best. A ten is very likely good.
		return deuce.Ten
	default:
		// A one-card draw, or a pat opponent representing a made hand.
		// Only a genuine low stands; a nine breaks.
		return deuce.Eight
	}
}

// Read is the opponent's most recent public draw count as one seat saw it,
// in table.Hand.OpponentDraw's terms. StreetsAgo carries the positional
// asymmetry: the button reads the current street (0), the big blind only the
// previous one (1), and the strategy must not pretend that away.
type Read struct {
	Count      int
	StreetsAgo int
	Known      bool
}

// Draw chooses the discard for a draw street. An empty list is standing pat.
//
// h1 never snows — never stands pat on a hand it will not improve in order to
// broadcast a false draw count. A generator driven purely by hand quality, as
// this one is, can never propose a snow: the move is worth making precisely
// when the hand is bad. It would need its own branch. Leaving it out is what
// keeps h1 legible, and it is also what makes h1's draw counts honest and
// therefore readable by anyone paying attention.
func Draw(hand *table.Hand) []cards.Card {
	if !hand.Complete() {
		return nil // nothing to reason about; stand pat rather than guess
	}
	count, streetsAgo, known := hand.OpponentDraw(hand.Street)
	return DrawDiscards(hand.Cards, hand.Street,
		Read{Count: count, StreetsAgo: streetsAgo, Known: known})
}

// DrawDiscards is the draw rule as a pure function of the visible facts —
// our five cards, the street, and the opponent's most recent draw count with
// its staleness. Exported so the equity rollouts can replay both seats'
// draws through the identical rule.
//
// The generated draw table answers first: a measured candidate index per
// (hand class, street × read context), from cmd/drawgen. Cells the
// generator saw too rarely to decide fall back to the structural rule —
// standPatFloor and DrawingKeep, h1's — so the table degrades toward the
// old behavior rather than toward noise.
func DrawDiscards(hand []cards.Card, street int, read Read) []cards.Card {
	if keep, ok := tableKeep(hand, street, read); ok {
		return Discards(hand, keep)
	}
	if deuce.Categorize(hand) >= standPatFloor(read) {
		return nil
	}
	return Discards(hand, DrawingKeep(hand))
}

// tableKeep consults the generated draw table, reporting false wherever the
// structural rule should decide instead.
func tableKeep(hand []cards.Card, street int, read Read) ([]cards.Rank, bool) {
	if len(hand) != deuce.HandSize || street < table.Draw1 || street > table.Draw3 {
		return nil, false
	}
	entry := drawTable[int(handclass.Of(hand))*NumDrawContexts+DrawContext(street, read)]
	if entry == drawNoData {
		return nil, false
	}
	candidates := DrawCandidates(hand)
	if int(entry) >= len(candidates) {
		return nil, false
	}
	return candidates[entry], true
}
