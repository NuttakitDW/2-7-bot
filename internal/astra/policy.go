// Package astra implements the 2-7-astra lineage. The inherited h3 draw
// tables supply discards; wagering uses draw advantage and mixed bluffs.
package astra

import (
	"math/rand/v2"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/policy"
	"github.com/nuttakit/2-7-bot/internal/table"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// Decide only uses private cards and public events, never opponent identity.
func Decide(state *table.Table, decision wire.Decision) wire.Action {
	return wire.Legalize(decision, propose(&state.Hand, decision, rand.Float64()), state.Hand.Cards)
}

func propose(h *table.Hand, d wire.Decision, mix float64) wire.Action {
	if !h.Complete() {
		return wire.Check()
	}
	if d.Kind == wire.DecisionDraw {
		return wire.Discard(policy.Draw(h))
	}
	if d.Kind != wire.DecisionWager {
		return wire.Check()
	}
	if h.Street == table.Predraw {
		chart := policy.Classify(h.Cards)
		if !h.Opened() {
			if chart.Open == policy.Raise {
				return wire.Raise(0)
			}
			return wire.Fold()
		}
		if chart.Defend == policy.Fold {
			return wire.Fold()
		}
		// A two-card draw may three-bet for position, but cannot keep
		// raising into a range strengthened by each additional wager.
		if chart.Defend == policy.Raise && (h.Wagers < 3 || chart.Shape >= policy.OneCardDraw) {
			return wire.Raise(0)
		}
		return wire.Call()
	}

	category := h.Category()
	opp, _, known := h.OpponentDraw(h.Street)
	if !known {
		opp = 1
	}
	ours, ownKnown := h.DrawCount(h.Seat, h.Street)
	if !ownKnown {
		ours = 2
	}
	if h.Street == table.Draw3 {
		return river(h, d, category, opp, mix)
	}

	// Price continuation from the keep the draw table will actually use.
	next := *h
	next.Street++
	draw := len(policy.Draw(&next))
	if d.Call == nil {
		if category >= deuce.Nine || (category == deuce.Ten && opp >= 2) {
			return wire.Raise(0)
		}
		if opp > 0 && ((draw <= 1 && ours < opp) || (draw <= 2 && opp >= 3)) {
			return wire.Raise(0)
		}
		if h.OnButton() && opp > 0 && mix < 0.22 {
			return wire.Raise(0)
		}
		return wire.Check()
	}
	if category == deuce.Seven || (category == deuce.Eight && (!h.FacingRaise() || smoothEight(h.Cards))) {
		return wire.Raise(0)
	}
	if category >= deuce.Nine || (category == deuce.Ten && !h.FacingRaise()) {
		return wire.Call()
	}
	if draw <= 1 || (h.Street == table.Draw1 && draw <= 2) {
		return wire.Call()
	}
	return wire.Fold()
}

func river(h *table.Hand, d wire.Decision, category deuce.Category, opp int, mix float64) wire.Action {
	if d.Call == nil {
		if category >= deuce.Eight || (category == deuce.Nine && opp > 0) || (category == deuce.Ten && opp >= 2) {
			return wire.Raise(0)
		}
		// Prefer failed draws with little showdown value as bluff candidates.
		// Never bluff a pat range; a checked draw range contains many misses.
		if opp > 0 && category < deuce.Ten {
			frequency := 0.38
			if h.OnButton() {
				frequency = 0.70
			}
			if mix < frequency {
				return wire.Raise(0)
			}
		}
		return wire.Check()
	}
	if category == deuce.Seven || (category == deuce.Eight && (!h.FacingRaise() || smoothEight(h.Cards))) {
		return wire.Raise(0)
	}
	if category >= deuce.Nine || (category == deuce.Ten && !h.FacingRaise()) {
		return wire.Call()
	}
	return wire.Fold()
}

func smoothEight(hand []cards.Card) bool {
	ranks := cards.DistinctRanks(hand)
	return len(ranks) == 5 && ranks[3] <= cards.Six
}
