package cfr

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/opp/cobalt"
	"github.com/nuttakit/2-7-bot/internal/policy"
	"github.com/nuttakit/2-7-bot/internal/table"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// View is everything one seat can see at a decision.
type View struct {
	// Hand is the seat's cards, sorted by rank.
	Hand [5]cards.Card
	Seat int
	// Node is the public tree node; Street, Facing, Wagers and CanRaise
	// restate what a model without the tree needs from it.
	Node     int32
	Street   int
	Facing   bool
	CanRaise bool
	Wagers   int
	// Drawn is each seat's draw count per street, -1 before drawing.
	Drawn [2]DrawCounts
	// LastAggr is the seat that made the hand's last wager, -1 for none.
	LastAggr int
	// Rand is a uniform draw in [0,1) for a model that mixes.
	Rand float64
}

// DrawCounts is one seat's draw count per street; -1 marks no draw yet.
type DrawCounts [Streets]int8

// Model is anything that plays a hand from a View. The trainer uses one as
// a fixed opponent; the match simulator plays any two against each other.
type Model interface {
	// Bet answers a betting node with Fold, Pass or Aggr. ok is false
	// where the model has no opinion, which the caller must resolve.
	Bet(v *View) (action int, ok bool)
	// Draw answers a draw node with a keep mask over the sorted hand.
	Draw(v *View) (keep uint8, ok bool)
}

// Heuristic wraps this repo's rule-based policies as Models: the current
// generation's policy package, and the frozen cobalt copy.
type Heuristic struct {
	Cobalt bool
}

// Bet plays the wrapped policy through the same wire.Decision the arena
// would send.
func (h Heuristic) Bet(v *View) (int, bool) {
	state := tableFor(v)
	decision := wire.Decision{Kind: wire.DecisionWager, Fold: v.Facing, Check: !v.Facing}
	call := uint64(SmallBet)
	if v.Facing {
		decision.Call = &call
	}
	if v.CanRaise {
		window := &wire.Range{MinTo: BigBet, MaxTo: BigBet}
		if v.Facing {
			decision.Raise = window
		} else {
			decision.Bet = window
		}
	}
	action := h.decide(state, decision, v.Rand)
	switch action.Kind {
	case wire.ActionFold:
		return Fold, true
	case wire.ActionBet, wire.ActionRaise:
		return Aggr, true
	default:
		return Pass, true
	}
}

// Draw plays the wrapped policy's discard and reads it back as a keep mask.
func (h Heuristic) Draw(v *View) (uint8, bool) {
	state := tableFor(v)
	decision := wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5}
	action := h.decide(state, decision, v.Rand)
	keep := uint8(1<<5 - 1)
	for _, card := range action.Cards {
		for i, held := range v.Hand {
			if held == card {
				keep &^= 1 << i
				break
			}
		}
	}
	return keep, true
}

func (h Heuristic) decide(state *table.Table, decision wire.Decision, mix float64) wire.Action {
	if h.Cobalt {
		return wire.Legalize(decision, cobalt.Propose(&state.Hand, decision, mix), state.Hand.Cards)
	}
	return policy.Decide(state, decision)
}

var streetLabels = [Streets]string{"predraw", "draw1", "draw2", "draw3"}

// tableFor rebuilds the table state a wire bot would have at this view,
// replaying the draw counts through Observe so the unexported per-seat
// table fills the same way it does off the event stream.
func tableFor(v *View) *table.Table {
	state := table.New()
	state.HandStart(wire.Message{Seat: v.Seat})
	for street := Draw1; street <= v.Street; street++ {
		state.Hand.Street = street
		for seat := 0; seat < 2; seat++ {
			if count := v.Drawn[seat][street]; count >= 0 {
				state.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: seat, Count: int(count)})
			}
		}
	}
	state.Hand.Street = v.Street
	state.Hand.Label = streetLabels[v.Street]
	state.Hand.Wagers = v.Wagers
	state.Hand.Cards = append([]cards.Card(nil), v.Hand[:]...)
	return state
}
