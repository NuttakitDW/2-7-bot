package zircon

import (
	_ "embed"
	"fmt"

	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/sixmaxclone"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

//go:embed clone_predraw_h5b.json
var clonePredrawH5B []byte

type Profile uint8

const (
	Baseline Profile = iota
	Generation2
	Generation3
)

type OverrideStats struct {
	Applied uint64
	Matrix  [3][3]uint64
}

func cloneModel() *sixmaxclone.Model {
	model, err := sixmaxclone.Decode(clonePredrawH5B)
	if err != nil {
		panic(fmt.Sprintf("zircon: embedded h5b model: %v", err))
	}
	return model
}

func NewBaseline() *Bot    { return newBot(Baseline, nil) }
func NewGeneration2() *Bot { return newBot(Generation2, cloneModel()) }
func NewGeneration3() *Bot { return newBot(Generation3, cloneModel()) }

func newBot(profile Profile, model *sixmaxclone.Model) *Bot {
	return &Bot{State: NewState(), profile: profile, clone: model}
}

func (b *Bot) OverrideStats() OverrideStats { return b.overrideStats }

func (b *Bot) clonePredraw(d wire.Decision, baseline wire.Action) (wire.Action, bool) {
	h := &b.State.Hand
	if (b.profile != Generation2 && b.profile != Generation3) || b.clone == nil || h.Street != Predraw || d.Kind != wire.DecisionWager ||
		d.Call == nil || *d.Call == 0 || h.StreetAggressions < 1 || !h.Complete() || b.State.HasAllIn() || h.SidePot ||
		!validSeat(h.Hero) || h.Category() >= deuce.Eight {
		return wire.Action{}, false
	}
	hero := h.Seats[h.Hero]
	if hero.Stack == 0 || hero.Contribution >= hero.Stack {
		return wire.Action{}, false
	}
	remaining := hero.Stack - hero.Contribution
	if *d.Call >= remaining {
		return wire.Action{}, false
	}
	features, err := sixmaxclone.BuildFeatures(sixmaxclone.Snapshot{
		Hand: h.Cards, Position: int(b.State.Position(h.Hero)), ActivePlayers: b.State.LiveOpponents() + 1,
		Aggressions: h.StreetAggressions, Calls: h.StreetCalls, OwnRaises: h.HeroPredrawRaises,
		Pot: h.Pot, Call: *d.Call, SmallBet: b.State.Match.BigBlind,
	})
	if err != nil {
		return wire.Action{}, false
	}
	prediction, ok := b.clone.Predict(features)
	if !ok {
		return wire.Action{}, false
	}
	var action wire.Action
	switch prediction {
	case sixmaxclone.Fold:
		if !d.Fold {
			return wire.Action{}, false
		}
		action = wire.Fold()
	case sixmaxclone.Passive:
		action = wire.Call()
	case sixmaxclone.Aggressive:
		if d.Raise == nil || cloneAggressionExhaustsStack(d.Raise.MinTo, hero.StreetCommit, remaining) {
			return wire.Action{}, false
		}
		action = wire.Raise(d.Raise.MinTo)
	default:
		return wire.Action{}, false
	}
	baselineClass, actionClass := cloneActionClass(baseline), cloneActionClass(action)
	if baselineClass != actionClass {
		b.overrideStats.Applied++
		b.overrideStats.Matrix[baselineClass][actionClass]++
	}
	return action, true
}

func cloneAggressionExhaustsStack(target, streetCommit, remaining uint64) bool {
	return target <= streetCommit || target-streetCommit >= remaining
}

func cloneActionClass(action wire.Action) int {
	switch action.Kind {
	case wire.ActionFold:
		return int(sixmaxclone.Fold)
	case wire.ActionBet, wire.ActionRaise:
		return int(sixmaxclone.Aggressive)
	default:
		return int(sixmaxclone.Passive)
	}
}
