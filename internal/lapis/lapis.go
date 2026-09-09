// Package lapis plays the MCCFR blueprint (internal/cfr) on the wire.
//
// It follows the arena's event stream through the public game tree, asks
// the blueprint at its own decisions, and falls back to the heuristic
// policy whenever the tree and the stream disagree or the blueprint has
// nothing trained for the spot. The fallback is the whole reason the bot
// can never be worse than confused: every path ends in wire.Legalize.
package lapis

import (
	_ "embed"
	"fmt"
	"math/rand/v2"
	"strconv"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/policy"
	"github.com/nuttakit/2-7-bot/internal/table"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

//go:embed blueprint.bin.gz
var blueprintData []byte

// Purify is the probability floor below which a blueprint action is
// dropped before sampling (cfr.Player), and Greedy makes the bot take the
// most likely trained action instead of sampling at all. Both are build
// flags because the right answer depends on how far the blueprint has
// converged: an unconverged average carries residual weight on actions it
// has all but abandoned, and playing those costs real chips.
var (
	Purify            = "0.05"
	Greedy            = "false"
	ResponseMinVisits = "50"
)

// lost marks a hand the tracker could not follow; the heuristic plays it.
const lost = -1

// Bot is one match's worth of state.
type Bot struct {
	Table  *table.Table
	tree   *cfr.Tree
	player cfr.Model
	rng    *rand.Rand

	node     int32
	lastAggr int
	drawn    [2]cfr.DrawCounts
	// fixed identifies the arena's constant big-blind card from our own
	// big-blind hands; the button then plays that card's strategy group.
	fixed cfr.FixedCard
	// Fallback answers the decisions the blueprint cannot: a lost hand,
	// or an untrained set. Nil means the heuristic policy.
	Fallback func(wire.Decision) wire.Action
	// Fallbacks counts decisions the fallback took, for diagnostics.
	Fallbacks int
}

// NewGreedy selects the modal blueprint action, matching the onyx-78 draw
// strategy. This selection is an empirical policy, not an equilibrium claim.
func NewGreedy() (*Bot, error) {
	b, err := New()
	if err != nil {
		return nil, err
	}
	b.player.(*cfr.Player).Greedy = true
	return b, nil
}

// NewModel plays an arbitrary strategy through the same tracker: a fitted
// opponent model, for sparring against a stand-in on the real engine.
func NewModel(m cfr.Model) *Bot {
	return &Bot{
		Table:  table.New(),
		tree:   cfr.BuildTree(),
		player: m,
		rng:    rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
		node:   lost,
	}
}

// NewEmpirical plays the modal action of an embedded fitted policy. This
// profile requires the build overlay to replace blueprintData's payload
// with empirical JSON instead of a compressed MCCFR blueprint.
func NewEmpirical() (*Bot, error) {
	m, err := cfr.DecodeEmpirical(blueprintData)
	if err != nil {
		return nil, fmt.Errorf("lapis empirical: %w", err)
	}
	return NewModel(m.Mode()), nil
}

// NewRiverResponse loads a fitted base policy with a trained sparse river
// response, packaged together in the embedded payload.
func NewRiverResponse() (*Bot, error) {
	tree := cfr.BuildTree()
	m, err := cfr.DecodeRiverResponse(blueprintData, tree)
	if err != nil {
		return nil, fmt.Errorf("lapis river response: %w", err)
	}
	m.Greedy, err = strconv.ParseBool(Greedy)
	if err != nil {
		return nil, fmt.Errorf("lapis: bad greedy %q", Greedy)
	}
	m.MinVisits, err = strconv.ParseUint(ResponseMinVisits, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("lapis: bad response minimum visits %q", ResponseMinVisits)
	}
	return NewModel(m), nil
}

// New decodes the embedded blueprint. It fails only on a build whose
// blueprint does not match its tree, or whose selection flags do not
// parse — both bugs worth refusing to run.
func New() (*Bot, error) {
	purify, err := strconv.ParseFloat(Purify, 64)
	if err != nil || purify < 0 || purify > 1 {
		return nil, fmt.Errorf("lapis: bad purify %q", Purify)
	}
	greedy, err := strconv.ParseBool(Greedy)
	if err != nil {
		return nil, fmt.Errorf("lapis: bad greedy %q", Greedy)
	}
	tree := cfr.BuildTree()
	abs := cfr.BuildAbstraction()
	layout := cfr.NewLayout(tree, abs)
	bp, err := cfr.Decode(blueprintData, layout)
	if err != nil {
		return nil, fmt.Errorf("lapis: %w", err)
	}
	return &Bot{
		Table:  table.New(),
		tree:   tree,
		player: &cfr.Player{Tree: tree, Abs: abs, Layout: layout, BP: bp, Purify: purify, Greedy: greedy},
		rng:    rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
		node:   lost,
	}, nil
}

// Hello records the match parameters.
func (b *Bot) Hello(msg wire.Message) {
	b.Table.Hello(msg)
	b.fixed = cfr.FixedCard{}
}

// HandStart resets the tracker at the tree's root.
func (b *Bot) HandStart(msg wire.Message) {
	b.Table.HandStart(msg)
	b.node = b.tree.Root
	b.lastAggr = -1
	for p := range b.drawn {
		for street := range b.drawn[p] {
			b.drawn[p][street] = -1
		}
	}
	if b.Table.Match.SeatCount != 2 {
		b.node = lost
	}
}

// Observe folds an event into both the table and the tree position.
func (b *Bot) Observe(event wire.Event) {
	b.Table.Observe(event)
	if b.node == lost {
		return
	}
	switch event.Kind {
	case wire.EventHandStart:
		// The tree assumes the button is seat 0, as the arena guarantees.
		if event.Button != cfr.Btn {
			b.node = lost
		}

	case wire.EventDealHole:
		if event.Seat == b.Table.Hand.Seat && event.Seat == cfr.BB && len(event.Cards) == 5 {
			b.fixed.ObserveBigBlind(cards.NewSet(event.Cards))
		}

	case wire.EventActed:
		node := &b.tree.Nodes[b.node]
		if node.Kind != cfr.KindBet || int(node.Actor) != event.Seat {
			b.node = lost
			return
		}
		action := actionOf(event.Action.Kind)
		found := false
		for i, act := range node.Acts {
			if int(act) == action {
				b.node = node.Next[i]
				found = true
				break
			}
		}
		if !found {
			b.node = lost
			return
		}
		if action == cfr.Aggr {
			b.lastAggr = event.Seat
		}

	case wire.EventStreetStart:
		node := &b.tree.Nodes[b.node]
		if event.Street == cfr.Predraw {
			return
		}
		if node.Kind != cfr.KindDraw || int(node.Street) != event.Street || node.Actor != cfr.BB {
			b.node = lost
		}

	case wire.EventDrawResult:
		node := &b.tree.Nodes[b.node]
		if node.Kind != cfr.KindDraw || int(node.Actor) != event.Seat || event.Seat < 0 || event.Seat > 1 {
			b.node = lost
			return
		}
		b.drawn[event.Seat][node.Street] = int8(event.Count)
		b.node = node.Next[0]
	}
}

func actionOf(kind string) int {
	switch kind {
	case wire.ActionFold:
		return cfr.Fold
	case wire.ActionBet, wire.ActionRaise:
		return cfr.Aggr
	default:
		return cfr.Pass
	}
}

// Decide answers one act.
func (b *Bot) Decide(decision wire.Decision) wire.Action {
	action, ok := b.propose(decision)
	if !ok {
		b.Fallbacks++
		if b.Fallback != nil {
			return b.Fallback(decision)
		}
		return policy.Decide(b.Table, decision)
	}
	return wire.Legalize(decision, action, b.Table.Hand.Cards)
}

func (b *Bot) propose(decision wire.Decision) (wire.Action, bool) {
	hand := &b.Table.Hand
	if b.node == lost || !hand.Complete() {
		return wire.Action{}, false
	}
	node := &b.tree.Nodes[b.node]
	if int(node.Actor) != hand.Seat {
		return wire.Action{}, false
	}
	sorted := cards.SortedByRank(hand.Cards)
	view := cfr.View{Seat: hand.Seat, Node: b.node, Street: int(node.Street),
		Drawn: b.drawn, LastAggr: b.lastAggr, Rand: b.rng.Float64()}
	copy(view.Hand[:], sorted)
	if hand.Seat == cfr.Btn {
		view.FixedGroup = b.fixed.Group()
	}
	if node.Kind == cfr.KindBet {
		view.Pot = node.Commit[0] + node.Commit[1]
		view.ToCall = max(0, node.Commit[1-hand.Seat]-node.Commit[hand.Seat])
		view.Facing = node.Facing
		view.Wagers = int(node.Wagers)
		view.CanRaise = node.Acts[len(node.Acts)-1] == cfr.Aggr
	}

	switch decision.Kind {
	case wire.DecisionWager:
		if node.Kind != cfr.KindBet || node.Facing != (decision.Call != nil) {
			return wire.Action{}, false
		}
		action, ok := b.player.Bet(&view)
		if !ok {
			return wire.Action{}, false
		}
		switch action {
		case cfr.Fold:
			return wire.Fold(), true
		case cfr.Aggr:
			return wire.Raise(0), true
		case cfr.Pass:
			if node.Facing {
				return wire.Call(), true
			}
			return wire.Check(), true
		}

	case wire.DecisionDraw:
		if node.Kind != cfr.KindDraw {
			return wire.Action{}, false
		}
		keep, ok := b.player.Draw(&view)
		if !ok {
			return wire.Action{}, false
		}
		discards := make([]cards.Card, 0, 5)
		for i, card := range sorted {
			if keep&(1<<i) == 0 {
				discards = append(discards, card)
			}
		}
		return wire.Discard(discards), true
	}
	return wire.Action{}, false
}
