package wire

import "github.com/nuttakit/2-7-bot/internal/cards"

// Legalize returns the closest legal action to the one the policy wants.
//
// This is the bot's safety net, and it exists because of how faults are
// handled. An illegal or malformed action is a fault, and under the arena's
// default `substitute` policy the bot does not crash — the arena quietly
// plays the minimal legal action on its behalf (a check if free, else a
// fold; a stand pat at a draw) and the match continues
// (WIRE_PROTOCOL.md:408-422). So a strategy bug does not announce itself; it
// just plays worse. Clamping here means the only way to see a fault is a bug
// in this file, and `faults: 0` in a spar report is a real gate rather than a
// formality.
//
// held is the bot's current hand, needed only to sanitise a discard list.
func Legalize(decision Decision, want Action, held []cards.Card) Action {
	switch decision.Kind {
	case DecisionWager:
		return legalizeWager(decision, want)
	case DecisionDraw:
		return legalizeDraw(decision, want, held)
	case DecisionBringIn:
		return legalizeBringIn(decision, want)
	default:
		// An unrecognized decision kind cannot be answered correctly by
		// definition. A check is the least-committing guess and mirrors
		// what the arena itself would substitute.
		return Check()
	}
}

// legalizeWager walks a preference list for the wanted verb and takes the
// first action the decision actually offers.
func legalizeWager(decision Decision, want Action) Action {
	for _, kind := range wagerPreference(want.Kind) {
		switch kind {
		case ActionRaise:
			if decision.Raise != nil {
				return Raise(clampTo(want.To, *decision.Raise))
			}
		case ActionBet:
			if decision.Bet != nil {
				return Bet(clampTo(want.To, *decision.Bet))
			}
		case ActionCall:
			if decision.Call != nil {
				return Call()
			}
		case ActionCheck:
			if decision.Check {
				return Check()
			}
		case ActionFold:
			if decision.Fold {
				return Fold()
			}
		}
	}
	return minimalWager(decision)
}

// wagerPreference orders the substitutes for each verb, most faithful first.
//
// The two passive verbs lean on an invariant from the spec: folding is only
// ever offered when there is something to call, so `Fold == false` implies
// there is no wager to face, which implies `Check == true`. Check and fold
// therefore always have each other as a fallback, and neither can strand.
func wagerPreference(kind string) []string {
	switch kind {
	case ActionRaise:
		return []string{ActionRaise, ActionBet, ActionCall, ActionCheck}
	case ActionBet:
		return []string{ActionBet, ActionRaise, ActionCheck, ActionCall}
	case ActionCall:
		return []string{ActionCall, ActionCheck}
	case ActionCheck:
		return []string{ActionCheck, ActionFold}
	case ActionFold:
		return []string{ActionFold, ActionCheck}
	default:
		return nil
	}
}

// minimalWager is the arena's own substitute: check if it is free, else fold.
func minimalWager(decision Decision) Action {
	switch {
	case decision.Check:
		return Check()
	case decision.Fold:
		return Fold()
	case decision.Call != nil:
		return Call()
	default:
		return Check()
	}
}

// clampTo pins a total commitment inside the offered window. In fixed limit
// MinTo == MaxTo, so this resolves any requested total — including a zero
// left unset by the caller — to the single legal number.
func clampTo(to uint64, window Range) uint64 {
	if to < window.MinTo {
		return window.MinTo
	}
	if to > window.MaxTo {
		return window.MaxTo
	}
	return to
}

// legalizeDraw sanitises a discard list: the cards must be distinct cards the
// bot actually holds, and at most MaxDiscards of them. Anything else is
// dropped rather than sent, and anything that is not a discard at all becomes
// a stand pat.
func legalizeDraw(decision Decision, want Action, held []cards.Card) Action {
	if want.Kind != ActionDiscard {
		return Discard(nil)
	}
	available := make([]cards.Card, len(held))
	copy(available, held)

	discards := make([]cards.Card, 0, len(want.Cards))
	for _, card := range want.Cards {
		if len(discards) >= decision.MaxDiscards {
			break
		}
		index := -1
		for i, candidate := range available {
			if candidate == card {
				index = i
				break
			}
		}
		if index < 0 {
			continue // not held, or already listed once
		}
		available = append(available[:index], available[index+1:]...)
		discards = append(discards, card)
	}
	return Discard(discards)
}

// legalizeBringIn answers a stud bring-in. 27td-fl never asks one; this
// exists so a mis-declared game degrades into legal play rather than faults.
func legalizeBringIn(decision Decision, want Action) Action {
	if want.Kind == ActionBet && decision.Complete != nil {
		return Bet(clampTo(want.To, *decision.Complete))
	}
	return BringIn()
}
