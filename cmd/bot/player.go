package main

import (
	"fmt"

	"github.com/nuttakit/2-7-bot/internal/beryl"
	"github.com/nuttakit/2-7-bot/internal/garnet"
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
	beryl   *beryl.Bot
	garnet  *garnet.Bot
	// blueprintOnly skips the mirroring overlays: the blueprint decides,
	// and only the river solver may override it.
	blueprintOnly bool
}

// Profiles. "onyx" is the heuristic bot; "model-bets" mixes fitted opponent
// mirroring into it; "learned" plays the embedded blueprint's modal action
// at every decision; "learned-bayes" plays the blueprint up to the last
// draw and solves the river against the fitted opponent model exactly;
// "azurite" samples the blueprint's mixture as trained and hands every
// set it never visited to the onyx lines, snows and all. "empirical" plays
// an embedded fitted policy through the same legal-action tracker.
func newRuntimeBot() (*runtimeBot, error) {
	switch playerProfile {
	case "onyx", "model-bets", "learned", "learned-bayes", "azurite", "empirical", "river-response", "river-response-blockers", "beryl", "garnet":
	default:
		return nil, fmt.Errorf("unknown player profile %q", playerProfile)
	}
	if playerProfile == "onyx" {
		base, err := onyx.New()
		return &runtimeBot{Bot: base}, err
	}
	if playerProfile == "beryl" {
		candidate, err := beryl.New()
		if err != nil {
			return nil, err
		}
		return &runtimeBot{Bot: candidate.Base, beryl: candidate}, nil
	}
	if playerProfile == "garnet" {
		candidate, err := garnet.New()
		if err != nil {
			return nil, err
		}
		return &runtimeBot{Bot: candidate.Base, garnet: candidate}, nil
	}
	if playerProfile == "azurite" || playerProfile == "empirical" || playerProfile == "river-response" || playerProfile == "river-response-blockers" {
		base, err := onyx.New()
		if err != nil {
			return nil, err
		}
		var learned *lapis.Bot
		if playerProfile == "empirical" {
			learned, err = lapis.NewEmpirical()
		} else if playerProfile == "river-response" {
			learned, err = lapis.NewRiverResponse()
		} else if playerProfile == "river-response-blockers" {
			learned, err = lapis.NewBlockerRiverResponse()
		} else {
			learned, err = lapis.New()
		}
		if err != nil {
			return nil, err
		}
		learned.Fallback = base.Decide
		return &runtimeBot{Bot: base, learned: learned, blueprintOnly: true}, nil
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
	if b.garnet != nil {
		b.garnet.Hello(m)
		return
	}
	if b.beryl != nil {
		b.beryl.Hello(m)
		return
	}
	b.Bot.Hello(m)
	if b.learned != nil {
		b.learned.Hello(m)
	}
}

func (b *runtimeBot) HandStart(m wire.Message) {
	if b.garnet != nil {
		b.garnet.HandStart(m)
		return
	}
	if b.beryl != nil {
		b.beryl.HandStart(m)
		return
	}
	b.Bot.HandStart(m)
	if b.learned != nil {
		b.learned.HandStart(m)
	}
}

func (b *runtimeBot) Observe(e wire.Event) {
	if b.garnet != nil {
		b.garnet.Observe(e)
		return
	}
	if b.beryl != nil {
		b.beryl.Observe(e)
		return
	}
	b.Bot.Observe(e)
	if b.learned != nil {
		b.learned.Observe(e)
	}
}

func (b *runtimeBot) Decide(d wire.Decision) wire.Action {
	if b.garnet != nil {
		return b.garnet.Decide(d)
	}
	if b.beryl != nil {
		return b.beryl.Decide(d)
	}
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
	if b.garnet != nil {
		return b.garnet.Fallbacks
	}
	if b.beryl != nil {
		return b.beryl.Fallbacks
	}
	if b.learned == nil {
		return 0
	}
	return b.learned.Fallbacks
}

func (b *runtimeBot) matchSummary() string {
	if b.garnet != nil {
		return fmt.Sprintf("%d blueprint fallbacks, garnet lookups exact=%d backoff=%d untrained=%d",
			b.garnet.Fallbacks, b.garnet.Exact, b.garnet.Backoffs, b.garnet.Untrained)
	}
	return fmt.Sprintf("%d blueprint fallbacks", b.fallbacks())
}
