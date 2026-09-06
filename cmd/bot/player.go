package main

import (
	"fmt"

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
}

func newRuntimeBot() (*runtimeBot, error) {
	if playerProfile != "onyx" && playerProfile != "model-bets" {
		return nil, fmt.Errorf("unknown player profile %q", playerProfile)
	}
	if playerProfile == "onyx" {
		base, err := onyx.New()
		return &runtimeBot{Bot: base}, err
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
