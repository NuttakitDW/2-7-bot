package onyx

import (
	"github.com/nuttakit/2-7-bot/internal/table"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// RefineLearned applies the response profile to a learned fallback action.
func (b *Bot) RefineLearned(d wire.Decision, a wire.Action, profile string) wire.Action {
	h := &b.Table.Hand
	if profile != "raise-aware" {
		return a
	}
	if d.Kind != wire.DecisionWager || !h.Complete() {
		return a
	}
	if d.Call != nil &&
		h.Street > table.Predraw && h.Wagers >= 2 && h.Complete() &&
		a.Kind == wire.ActionRaise && !nuts(h.Cards) {
		return wire.Legalize(d, wire.Call(), h.Cards)
	}
	return a
}
