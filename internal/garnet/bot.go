package garnet

import (
	_ "embed"
	"fmt"
	"math/rand/v2"

	"github.com/nuttakit/2-7-bot/internal/beryl"
	"github.com/nuttakit/2-7-bot/internal/lapis"
	"github.com/nuttakit/2-7-bot/internal/onyx"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// These tracked placeholders deliberately fail closed. Release and native
// Garnet builds overlay both with generated, mutually hashed assets.
//
//go:embed ranking.json
var embeddedRanking []byte

//go:embed policy.json
var embeddedPolicy []byte

type Bot struct {
	Base *onyx.Bot

	reference  *beryl.Bot
	gate       *Gate
	bucketer   *Bucketer
	policy     *Policy
	rng        *rand.Rand
	policySeed uint64

	Exact     uint64
	Backoffs  uint64
	Untrained uint64
	// ForcedPassive counts admitted predraw decisions with no aggressive
	// legal action. Training has no information set for these capped nodes.
	ForcedPassive uint64
	Fallbacks     int
}

func New() (*Bot, error) {
	ranking, err := DecodeRanking(embeddedRanking)
	if err != nil {
		return nil, fmt.Errorf("garnet: ranking: %w", err)
	}
	rankingHash, err := RankingHash(ranking)
	if err != nil {
		return nil, fmt.Errorf("garnet: ranking hash: %w", err)
	}
	referenceHash, err := lapis.EmbeddedPolicyHash()
	if err != nil {
		return nil, fmt.Errorf("garnet: spinel policy hash: %w", err)
	}
	if ranking.ReferencePolicyHash != referenceHash {
		return nil, fmt.Errorf("garnet: ranking reference policy hash %q, embedded Spinel policy hash %q",
			ranking.ReferencePolicyHash, referenceHash)
	}
	policy, err := DecodePolicy(embeddedPolicy)
	if err != nil {
		return nil, fmt.Errorf("garnet: policy: %w", err)
	}
	if err := policy.Validate(rankingHash); err != nil {
		return nil, fmt.Errorf("garnet: policy: %w", err)
	}
	percentagesHash, err := PercentagesHash(policy.Percentages)
	if err != nil {
		return nil, fmt.Errorf("garnet: percentages hash: %w", err)
	}
	if percentagesHash != policy.PercentagesHash {
		return nil, fmt.Errorf("garnet: policy percentages hash %q, want %q", policy.PercentagesHash, percentagesHash)
	}
	gate, err := NewGate(ranking, policy.Percentages)
	if err != nil {
		return nil, err
	}
	bucketer, err := NewBucketer(ranking, policy.BucketCount)
	if err != nil {
		return nil, err
	}
	reference, err := beryl.New()
	if err != nil {
		return nil, err
	}
	return newBot(reference, gate, bucketer, policy), nil
}

func newBot(reference *beryl.Bot, gate *Gate, bucketer *Bucketer, policy *Policy) *Bot {
	bot := &Bot{
		Base: reference.Base, reference: reference, gate: gate, bucketer: bucketer,
		policy: policy, policySeed: policy.Seed,
	}
	bot.reseed(0, 0)
	reference.PredrawOverride = bot.predraw
	return bot
}

func (b *Bot) Hello(message wire.Message) { b.reference.Hello(message) }

func (b *Bot) HandStart(message wire.Message) {
	b.reseed(message.HandNo, message.Seat)
	b.reference.HandStart(message)
}

func (b *Bot) Observe(event wire.Event) { b.reference.Observe(event) }

func (b *Bot) Decide(decision wire.Decision) wire.Action {
	before := b.reference.Fallbacks
	action := b.reference.Decide(decision)
	b.Fallbacks += b.reference.Fallbacks - before
	return action
}

func (b *Bot) predraw(view beryl.PredrawView) (beryl.PredrawChoice, bool) {
	position := Position(view.Position)
	if position >= Positions || b.gate == nil || b.bucketer == nil || b.policy == nil {
		b.Untrained++
		return beryl.PredrawChoice{}, false
	}
	context := ContextFor(view)
	if !view.EntryDecided && !b.gate.Admit(position, context, view.Hand) {
		return beryl.PredrawChoice{Action: excludedAction(view.Decision), Admitted: false}, true
	}
	if view.EntryDecided && !view.Admitted {
		return beryl.PredrawChoice{Action: excludedAction(view.Decision), Admitted: false}, true
	}
	if view.Decision.Raise == nil && view.Decision.Bet == nil {
		b.ForcedPassive++
		return beryl.PredrawChoice{Action: passiveAction(view.Decision), Admitted: true}, true
	}
	bucket, ok := b.bucketer.Bucket(view.Hand)
	if !ok {
		b.Untrained++
		return beryl.PredrawChoice{Action: passiveAction(view.Decision), Admitted: true}, true
	}
	strategy, kind := b.policy.Lookup(KeyFor(view, bucket), position, context, bucket)
	switch kind {
	case LookupExact:
		b.Exact++
	case LookupBackoff:
		b.Backoffs++
	case LookupUntrained:
		b.Untrained++
	}
	action := passiveAction(view.Decision)
	if kind != LookupUntrained && b.rng.Float64() < strategy.Raise {
		if view.Decision.Raise != nil {
			action = wire.Raise(view.Decision.Raise.MinTo)
		} else if view.Decision.Bet != nil {
			action = wire.Bet(view.Decision.Bet.MinTo)
		}
	}
	return beryl.PredrawChoice{Action: action, Admitted: true}, true
}

func excludedAction(decision wire.Decision) wire.Action {
	if decision.Fold {
		return wire.Fold()
	}
	return wire.Check()
}

func passiveAction(decision wire.Decision) wire.Action {
	if decision.Call != nil {
		return wire.Call()
	}
	return wire.Check()
}

func (b *Bot) reseed(handNo uint64, seat int) {
	first := b.policySeed ^ handNo*0x9E3779B97F4A7C15 ^ uint64(seat+1)*0xD1B54A32D192ED03
	b.rng = rand.New(rand.NewPCG(first, first^0xA0761D6478BD642F))
}
