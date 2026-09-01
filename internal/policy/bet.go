package policy

import (
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/table"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// The betting rules, heads-up.
//
// Both of this repo's retired bots failed here rather than at hand reading.
// nutt-27td-m1 ran vpip 83% / pfr 66% and won 28% of its showdowns — it bet
// hands that could not win. nutt-27td-fl ran vpip 64-72% / pfr 27-30% and won
// 11% — it called with hands that could not win. So the two things these
// rules have to get right are: do not bet without a hand, and do not call
// without one either.
//
// Sizing never appears below because there is none. 27td-fl is fixed limit,
// so every wager range arrives with min_to == max_to and the only choice is
// the verb.

// predraw plays the opening round from the range chart.
func predraw(hand *table.Hand, chart Chart) wire.Action {
	if !hand.Opened() {
		// Nobody has raised: either we are the button facing only the big
		// blind, or the big blind with its option. The chart's opening
		// range is raise-or-fold — limping is not in it.
		if chart.Open == Raise {
			return wire.Raise(0)
		}
		// Folding for free is never legal; the guard turns this into a
		// check when we are the big blind with nothing to call.
		return wire.Fold()
	}
	switch chart.Defend {
	case Raise:
		return wire.Raise(0)
	case Call:
		return wire.Call()
	default:
		return wire.Fold()
	}
}

// betFloor is the hand needed to put chips in voluntarily, read against the
// opponent's most recent draw count — the mirror of standPatFloor in draw.go
// and motivated the same way.
//
// A seven or eight always bets. A nine bets only against an opponent drawing
// two or more. A ten never bets: with a marginal made hand at the end, the
// sources say check rather than bet, and a ten is exactly that hand.
func betFloor(opponentDrew int, known bool) deuce.Category {
	if known && opponentDrew >= 2 {
		return deuce.Nine
	}
	return deuce.Eight
}

// drawStreet plays a betting round after a draw.
func drawStreet(hand *table.Hand, decision wire.Decision) wire.Action {
	category := hand.Category()
	opponentDrew, _, known := hand.OpponentDraw(hand.Street)
	facing := decision.Call != nil

	if category >= betFloor(opponentDrew, known) {
		// Strong enough to wager, and in fixed limit that means every
		// time it is offered: when the raise cap is reached the arena
		// simply stops offering `raise` and the guard falls back to a
		// call. There is nothing to ration.
		return wire.Raise(0)
	}

	if !facing {
		return wire.Check()
	}
	if continues(hand, category) {
		return wire.Call()
	}
	return wire.Fold()
}

// continues decides whether a hand that is not worth betting is worth paying
// for. This is the calling-station guard: everything that reaches here is
// behind, so it has to justify the call with either a made hand or a real
// draw and a street left to use it on.
func continues(hand *table.Hand, category deuce.Category) bool {
	if category >= deuce.Ten {
		// A made ten calls a bet, but not a raise once the big bets
		// start — that is where paying off made hands gets expensive.
		return !(hand.FacingRaise() && hand.Street >= table.Draw2)
	}
	// Not made. The only reason to pay is a draw we still get to take.
	//
	// Deliberately the structural DrawingKeep, not the generated draw
	// table: h3 changed the draw rule and nothing else, so its betting is
	// bit-identical to h2's and a match delta measures the draws alone —
	// the isolation the h2 selection report proved out. The price is a
	// known incoherence (the table may keep four where this reasons about
	// three); pricing defends and continues together against the table is
	// h4's change, not a drive-by edit here.
	switch hand.Street {
	case table.Draw1:
		// Two draws left, small bets: a one- or two-card draw continues.
		return len(DrawingKeep(hand.Cards)) >= 3
	case table.Draw2:
		// One draw left and big bets: a one-card draw only.
		return len(DrawingKeep(hand.Cards)) >= 4
	default:
		// Draw3's betting round is after the final draw. There is no
		// draw left to pay for, so a hand that is not made folds.
		return false
	}
}
