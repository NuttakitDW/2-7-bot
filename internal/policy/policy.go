package policy

import (
	"github.com/nuttakit/2-7-bot/internal/table"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// Decide answers one `act`.
//
// The strategy only ever *proposes*; wire.Legalize has the last word. That
// split is what makes `faults: 0` a real gate rather than a hope — a bug in
// the rules below can make the bot play badly, but it cannot make it play
// illegally, and the two failure modes stay distinguishable in a match
// report.
func Decide(state *table.Table, decision wire.Decision) wire.Action {
	return wire.Legalize(decision, propose(state, decision), state.Hand.Cards)
}

func propose(state *table.Table, decision wire.Decision) wire.Action {
	hand := &state.Hand
	switch decision.Kind {
	case wire.DecisionDraw:
		return wire.Discard(Draw(hand))

	case wire.DecisionWager:
		if !hand.Complete() {
			// We were asked to bet without a readable hand. Take the
			// free option if there is one and pay nothing if there is
			// not; the guard resolves either into something legal.
			return wire.Check()
		}
		if hand.Street == table.Predraw {
			return predraw(hand, Classify(hand.Cards))
		}
		return drawStreet(hand, decision)

	default:
		// A bring-in, or something a future arena adds. Neither belongs
		// to this game; answer minimally and keep playing.
		return wire.Check()
	}
}
