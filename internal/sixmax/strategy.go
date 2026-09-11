package sixmax

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/policy"
	"github.com/nuttakit/2-7-bot/internal/sixmaxclone"
	"github.com/nuttakit/2-7-bot/internal/sixmaxrange"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// Config keeps the first ring strategy's main hypotheses explicit and easy
// to tune from local and hosted evidence.
type Config struct {
	ValueAggression         bool
	EarlyTenBreak           bool
	LateOpenTwoDraw         bool
	PricedOpenDefense       bool
	DefensePriceDenominator uint64
	BreakNineAtLive         int
	BreakTenAtLive          int
	RiverBetFloor           deuce.Category
	RiverCallFloor          deuce.Category
	RiverRaiseFloor         deuce.Category
	RiverCallMargin         float64
}

func DefaultConfig() Config {
	return Config{LateOpenTwoDraw: true, PricedOpenDefense: true, DefensePriceDenominator: 5,
		BreakNineAtLive: 3, BreakTenAtLive: 2,
		RiverBetFloor: deuce.Eight, RiverCallFloor: deuce.Nine, RiverRaiseFloor: deuce.Seven}
}

type Bot struct {
	State  *State
	Config Config
	Ranges *sixmaxrange.Model
	Clone  *sixmaxclone.Model
}

func New(config Config) *Bot            { return &Bot{State: NewState(), Config: config} }
func (b *Bot) Hello(m wire.Message)     { b.State.Hello(m) }
func (b *Bot) HandStart(m wire.Message) { b.State.HandStart(m) }
func (b *Bot) Observe(e wire.Event)     { b.State.Observe(e) }

func (b *Bot) Decide(d wire.Decision) wire.Action {
	baseline := wire.Legalize(d, b.propose(d), b.State.Hand.Cards)
	if action, ok := b.clonePredraw(d); ok {
		return wire.Legalize(d, action, b.State.Hand.Cards)
	}
	if action, ok := b.rangeCall(d, baseline); ok {
		return wire.Legalize(d, action, b.State.Hand.Cards)
	}
	return baseline
}

func (b *Bot) clonePredraw(d wire.Decision) (wire.Action, bool) {
	h := &b.State.Hand
	if b.Clone == nil || h.Street != Predraw || d.Kind != wire.DecisionWager || !h.Complete() || b.State.HasAllIn() || h.SidePot {
		return wire.Action{}, false
	}
	if !validSeat(h.Hero) {
		return wire.Action{}, false
	}
	hero := h.Seats[h.Hero]
	if hero.Stack == 0 || hero.Contribution >= hero.Stack {
		return wire.Action{}, false
	}
	remaining := hero.Stack - hero.Contribution
	call := uint64(0)
	if d.Call != nil {
		call = *d.Call
		if call >= remaining {
			return wire.Action{}, false
		}
	}
	features, err := sixmaxclone.BuildFeatures(sixmaxclone.Snapshot{Hand: h.Cards, Position: int(b.State.Position(h.Hero)),
		ActivePlayers: b.State.LiveOpponents() + 1, Aggressions: h.StreetAggressions, Calls: h.StreetCalls,
		OwnRaises: h.HeroPredrawRaises, Pot: h.Pot, Call: call, SmallBet: b.State.Match.BigBlind})
	if err != nil {
		return wire.Action{}, false
	}
	action, ok := b.Clone.Predict(features)
	if !ok {
		return wire.Action{}, false
	}
	if action == sixmaxclone.Fold && exactNuts(h.Cards) {
		return wire.Action{}, false
	}
	switch action {
	case sixmaxclone.Fold:
		if d.Fold {
			return wire.Fold(), true
		}
	case sixmaxclone.Passive:
		if d.Call != nil {
			return wire.Call(), true
		}
		if d.Check {
			return wire.Check(), true
		}
	case sixmaxclone.Aggressive:
		if d.Raise != nil {
			if cloneAggressionExhaustsStack(d.Raise.MinTo, hero.StreetCommit, remaining) {
				return wire.Action{}, false
			}
			return wire.Raise(d.Raise.MinTo), true
		}
		if d.Bet != nil {
			if cloneAggressionExhaustsStack(d.Bet.MinTo, hero.StreetCommit, remaining) {
				return wire.Action{}, false
			}
			return wire.Bet(d.Bet.MinTo), true
		}
	}
	return wire.Action{}, false
}

func cloneAggressionExhaustsStack(target, streetCommit, remaining uint64) bool {
	return target <= streetCommit || target-streetCommit >= remaining
}

func (b *Bot) rangeCall(d wire.Decision, baseline wire.Action) (wire.Action, bool) {
	h := &b.State.Hand
	if b.Ranges == nil || baseline.Kind == wire.ActionRaise || h.Street != Draw3 || d.Kind != wire.DecisionWager ||
		d.Call == nil || *d.Call == 0 || b.State.PlayersBehind() != 0 || b.State.HasAllIn() || h.SidePot || !h.Complete() {
		return wire.Action{}, false
	}
	if !validSeat(h.Hero) {
		return wire.Action{}, false
	}
	hero := h.Seats[h.Hero]
	if hero.Stack == 0 || hero.Contribution >= hero.Stack || *d.Call >= hero.Stack-hero.Contribution {
		return wire.Action{}, false
	}
	distributions := make([]sixmaxrange.Distribution, 0, SeatCount-1)
	for seat := range h.Seats {
		if seat == h.Hero || h.Seats[seat].Folded {
			continue
		}
		ctx, known := b.State.RangeContext(seat)
		if !known {
			return wire.Action{}, false
		}
		distribution, supported := b.Ranges.Lookup(ctx)
		if !supported {
			return wire.Action{}, false
		}
		distributions = append(distributions, distribution)
	}
	q := sixmaxrange.ShowdownEquity(deuce.Eval(h.Cards), distributions)
	ev := q*float64(h.Pot+*d.Call) - float64(*d.Call)
	if ev > b.Config.RiverCallMargin {
		return wire.Call(), true
	}
	return wire.Fold(), true
}

func (b *Bot) propose(d wire.Decision) wire.Action {
	if d.Kind == wire.DecisionDraw {
		return wire.Discard(b.draw())
	}
	if d.Kind != wire.DecisionWager || !b.State.Hand.Complete() {
		return wire.Check()
	}
	if b.State.Hand.Street == Predraw {
		return b.predraw(d)
	}
	return b.postdraw(d)
}

func (b *Bot) predraw(d wire.Decision) wire.Action {
	h := &b.State.Hand
	category := h.Category()
	keep := policy.DrawingKeep(h.Cards)
	draw := len(h.Cards) - len(keep)
	strong := category >= deuce.Nine || draw == 1 || (draw == 2 && containsRank(keep, cards.Two))
	late := b.State.Position(h.Hero) == Cutoff || b.State.Position(h.Hero) == Button
	playable := strong || (late && b.Config.LateOpenTwoDraw && draw == 2)

	switch {
	case h.StreetAggressions >= 2:
		if category >= deuce.Eight || draw == 1 {
			return wire.Call()
		}
		if b.Config.PricedOpenDefense && h.StreetAggressions == 2 && h.HeroPredrawRaises == 1 &&
			cleanStrongTwoDraw(keep) && affordable(h.Pot, d.Call, b.Config.DefensePriceDenominator) {
			return wire.Call()
		}
		return wire.Fold()
	case h.StreetAggressions == 1:
		if category >= deuce.Eight {
			return wire.Raise(0)
		}
		if strong {
			return wire.Call()
		}
		return wire.Fold()
	case h.StreetCalls > 0:
		if strong {
			return wire.Raise(0)
		}
		if playable {
			return wire.Call()
		}
		return wire.Fold()
	case playable:
		return wire.Raise(0)
	default:
		return wire.Fold()
	}
}

func (b *Bot) draw() []cards.Card {
	h := &b.State.Hand
	if !h.Complete() {
		return nil
	}
	if b.Config.EarlyTenBreak {
		if discard, ok := EarlyTenDraw(b.State); ok {
			return discard
		}
	}
	category := h.Category()
	if category >= deuce.Eight {
		return nil
	}
	if category == deuce.Nine && !b.breakMade(b.Config.BreakNineAtLive) {
		return nil
	}
	if category == deuce.Ten && !b.breakMade(b.Config.BreakTenAtLive) {
		return nil
	}
	return policy.Discards(h.Cards, policy.DrawingKeep(h.Cards))
}

func (b *Bot) breakMade(liveThreshold int) bool {
	if b.State.Hand.Street > Draw3 {
		return false
	}
	pat := false
	for _, read := range b.State.LiveOpponentDraws() {
		if read.Known && read.Count == 0 {
			pat = true
			break
		}
	}
	return pat || b.State.LiveOpponents() >= liveThreshold
}

func (b *Bot) postdraw(d wire.Decision) wire.Action {
	h := &b.State.Hand
	category := h.Category()
	facing := d.Call != nil
	if !facing && b.Config.ValueAggression &&
		(ValueNineLead(b.State) || (StrongDrawLead(b.State) && len(b.draw()) == 1)) {
		return wire.Bet(0)
	}
	if h.Street == Draw3 {
		return b.river(d, category)
	}

	patPressure := b.patPressure()
	if category >= deuce.Eight {
		if facing && patPressure && category == deuce.Eight {
			return wire.Call()
		}
		return wire.Raise(0)
	}
	if !facing {
		return wire.Check()
	}
	if category >= deuce.Nine {
		return wire.Call()
	}
	keep := policy.DrawingKeep(h.Cards)
	draw := len(h.Cards) - len(keep)
	if draw <= 1 {
		return wire.Call()
	}
	if h.Street == Draw1 && draw == 2 && affordable(h.Pot, d.Call, 4) {
		return wire.Call()
	}
	return wire.Fold()
}

func (b *Bot) river(d wire.Decision, category deuce.Category) wire.Action {
	facing := d.Call != nil
	pressure := b.patPressure()
	behind := b.State.PlayersBehind()
	callFloor := b.Config.RiverCallFloor
	betFloor := b.Config.RiverBetFloor
	if b.State.LiveOpponents() >= 3 || behind >= 2 || pressure {
		callFloor, betFloor = deuce.Eight, deuce.Eight
	}
	if facing {
		if exactNuts(b.State.Hand.Cards) && behind == 0 {
			return wire.Raise(0)
		}
		if category >= b.Config.RiverRaiseFloor && !pressure && behind == 0 {
			return wire.Raise(0)
		}
		if category >= callFloor {
			return wire.Call()
		}
		return wire.Fold()
	}
	if category >= betFloor {
		return wire.Bet(0)
	}
	return wire.Check()
}

func cleanStrongTwoDraw(keep []cards.Rank) bool {
	return len(keep) == 3 && keep[0] == cards.Two && keep[len(keep)-1] <= cards.Seven
}

func exactNuts(hand []cards.Card) bool {
	if deuce.Categorize(hand) != deuce.Seven {
		return false
	}
	ranks := cards.DistinctRanks(hand)
	want := [...]cards.Rank{cards.Two, cards.Three, cards.Four, cards.Five, cards.Seven}
	if len(ranks) != len(want) {
		return false
	}
	for i := range want {
		if ranks[i] != want[i] {
			return false
		}
	}
	return true
}

func (b *Bot) patPressure() bool {
	for _, read := range b.State.LiveOpponentDraws() {
		if read.Known && read.Count == 0 {
			return true
		}
	}
	return false
}

func containsRank(ranks []cards.Rank, rank cards.Rank) bool {
	for _, r := range ranks {
		if r == rank {
			return true
		}
	}
	return false
}

func affordable(pot uint64, call *uint64, denominator uint64) bool {
	return call != nil && denominator > 0 && (*call == 0 || *call*denominator <= pot+*call)
}
