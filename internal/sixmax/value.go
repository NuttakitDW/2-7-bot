package sixmax

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/policy"
)

// ValueNineLead reports the narrow early-street spot where a genuine nine
// can value-bet opponents whose current draw counts show they are still
// drawing. It intentionally rejects stale or unknown draw reads.
func ValueNineLead(state *State) bool {
	if state == nil || (state.Hand.Street != Draw1 && state.Hand.Street != Draw2) ||
		state.Hand.Category() != deuce.Nine {
		return false
	}
	draws, ok := currentLiveDraws(state)
	if !ok || len(draws) < 1 || len(draws) > 2 {
		return false
	}
	minimum := 1
	if state.Hand.Street == Draw1 && len(draws) == 2 {
		minimum = 2
	}
	for _, draw := range draws {
		if draw < minimum {
			return false
		}
	}
	return true
}

// StrongDrawLead reports a heads-up draw-one spot where the hero has a clean
// four-card eight draw and the opponent just drew at least two cards.
func StrongDrawLead(state *State) bool {
	if state == nil || state.Hand.Street != Draw1 || !state.Hand.Complete() ||
		state.Hand.Category() >= deuce.Eight {
		return false
	}
	draws, ok := currentLiveDraws(state)
	if !ok || len(draws) != 1 || draws[0] < 2 {
		return false
	}
	return cleanFourCardEightDraw(policy.DrawingKeep(state.Hand.Cards))
}

// EarlyTenDraw returns the single ten to discard from a genuine smooth ten
// on draw one or draw two. The boolean distinguishes this override from the
// ordinary draw policy. This helper is deliberately unwired.
func EarlyTenDraw(state *State) ([]cards.Card, bool) {
	if state == nil || (state.Hand.Street != Draw1 && state.Hand.Street != Draw2) ||
		state.Hand.Category() != deuce.Ten {
		return nil, false
	}
	keep := policy.DrawingKeep(state.Hand.Cards)
	if !cleanFourCardEightDraw(keep) {
		return nil, false
	}
	discard := policy.Discards(state.Hand.Cards, keep)
	if len(discard) != 1 || discard[0].Rank != cards.Ten {
		return nil, false
	}
	return discard, true
}

func currentLiveDraws(state *State) ([]int, bool) {
	draws := make([]int, 0, SeatCount-1)
	for seat := range state.Hand.Seats {
		if seat == state.Hand.Hero || state.Hand.Seats[seat].Folded {
			continue
		}
		count := state.Hand.Seats[seat].Draws[state.Hand.Street]
		if count < 0 {
			return nil, false
		}
		draws = append(draws, count)
	}
	return draws, true
}

func cleanFourCardEightDraw(keep []cards.Rank) bool {
	if len(keep) != 4 || keep[0] != cards.Two || keep[len(keep)-1] > cards.Eight {
		return false
	}
	for i := 1; i < len(keep); i++ {
		if keep[i] <= keep[i-1] {
			return false
		}
	}
	return true
}
