package main

import (
	"fmt"

	"github.com/nuttakit/2-7-bot/internal/cards"

	"github.com/nuttakit/2-7-bot/internal/lapis"
	"github.com/nuttakit/2-7-bot/internal/onyx"
	"github.com/nuttakit/2-7-bot/internal/table"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// playerProfile is fixed per artifact through the build flags.
var playerProfile = "onyx"

type runtimeBot struct {
	*onyx.Bot
	learned *lapis.Bot
	// blueprintOnly skips the mirroring overlays: the blueprint decides,
	// and only the river solver may override it.
	blueprintOnly bool
}

// Profiles. "onyx" is the heuristic bot; "model-bets" mixes fitted opponent
// mirroring into it; "learned" plays the embedded blueprint at every
// decision; "learned-bayes" plays the blueprint up to the last draw and
// solves the river against the fitted opponent model exactly.
func newRuntimeBot() (*runtimeBot, error) {
	switch playerProfile {
	case "onyx", "model-bets", "learned", "learned-bayes":
	default:
		return nil, fmt.Errorf("unknown player profile %q", playerProfile)
	}
	if playerProfile == "onyx" {
		base, err := onyx.New()
		return &runtimeBot{Bot: base}, err
	}
	if playerProfile == "learned" || playerProfile == "learned-bayes" {
		learned, err := lapis.NewGreedy()
		if err != nil {
			return nil, err
		}
		var base *onyx.Bot
		if playerProfile == "learned" {
			base, err = onyx.New()
		} else {
			base, err = onyx.NewModeled()
		}
		if err != nil {
			return nil, err
		}
		return &runtimeBot{Bot: base, learned: learned, blueprintOnly: true}, nil
	}
	base, err := onyx.NewModeled()
	if err != nil {
		return nil, err
	}
	learned, err := lapis.NewGreedy()
	if err != nil {
		return nil, err
	}
	return &runtimeBot{Bot: base, learned: learned}, nil
}

func (b *runtimeBot) Hello(m wire.Message) {
	b.Bot.Hello(m)
	if b.learned != nil {
		b.learned.Hello(m)
	}
}

func (b *runtimeBot) HandStart(m wire.Message) {
	b.Bot.HandStart(m)
	if b.learned != nil {
		b.learned.HandStart(m)
	}
}

func (b *runtimeBot) Observe(e wire.Event) {
	b.Bot.Observe(e)
	if b.learned != nil {
		b.learned.Observe(e)
	}
}

func (b *runtimeBot) Decide(d wire.Decision) wire.Action {
	if b.learned == nil {
		return b.Bot.Decide(d)
	}
	if b.blueprintOnly {
		if playerProfile == "learned-bayes" && b.Table.Hand.Street == table.Draw3 && d.Kind == wire.DecisionWager {
			if a, ok := b.Bot.RiverSolve(d); ok {
				return a
			}
		}
		return b.learned.Decide(d)
	}
	if b.Table.Hand.Street == table.Predraw {
		if a, ok := b.Bot.MirrorPredraw(d); ok {
			return a
		}
		return b.Bot.Decide(d)
	}
	if a, ok := b.Bot.MirrorDraw(d); ok {
		return b.Bot.RefineFinalDraw(d, a)
	}
	if a, ok := b.Bot.MirrorPostdraw(d); ok {
		return a
	}
	return b.Bot.RefineFinalDraw(d, b.Bot.RefineLearned(d, b.learned.Decide(d), "raise-aware"))
}

// fallbacks counts blueprint decisions the heuristic had to take.
func (b *runtimeBot) fallbacks() int {
	if b.learned == nil {
		return 0
	}
	return b.learned.Fallbacks
}

// fixedCard is the identified constant big-blind card, for diagnostics.
func (b *runtimeBot) fixedCard() []cards.Card {
	if b.learned == nil {
		return nil
	}
	return b.learned.FixedCard().Append(nil)
}
