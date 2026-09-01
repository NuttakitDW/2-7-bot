package policy

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/table"
)

// The draw rule: h1's own, untuned.
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
func standPatFloor(opponentDrew int, known bool) deuce.Category {
	if !known {
		// No read yet — heads-up this is the big blind before the button
		// has drawn. Hold a nine, break a ten.
		return deuce.Nine
	}
	switch {
	case opponentDrew >= 2:
		// They are two cards away at best. A ten is very likely good.
		return deuce.Ten
	default:
		// A one-card draw, or a pat opponent representing a made hand.
		// Only a genuine low stands; a nine breaks.
		return deuce.Eight
	}
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
	opponentDrew, _, known := hand.OpponentDraw(hand.Street)
	return DrawDiscards(hand.Cards, opponentDrew, known)
}

// DrawDiscards is the draw rule as a pure function of the visible facts —
// our five cards and the opponent's most recent draw count. Exported so the
// equity rollouts can replay both seats' draws through the identical rule.
func DrawDiscards(hand []cards.Card, opponentDrew int, known bool) []cards.Card {
	if deuce.Categorize(hand) >= standPatFloor(opponentDrew, known) {
		return nil
	}
	return Discards(hand, DrawingKeep(hand))
}
