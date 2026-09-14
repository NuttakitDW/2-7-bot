// Package lapis plays the MCCFR blueprint (internal/cfr) on the wire.
//
// It follows the arena's event stream through the public game tree, asks
// the blueprint at its own decisions, and falls back to the heuristic
// policy whenever the tree and the stream disagree or the blueprint has
// nothing trained for the spot. The fallback is the whole reason the bot
// can never be worse than confused: every path ends in wire.Legalize.
package lapis

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
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

// EmbeddedPolicyHash identifies the exact policy payload compiled into this
// process. Composite profiles use it to reject rankings built for another
// Spinel policy before playing a hand.
func EmbeddedPolicyHash() (string, error) {
	if len(blueprintData) == 0 {
		return "", fmt.Errorf("lapis: empty embedded policy")
	}
	digest := sha256.Sum256(blueprintData)
	return hex.EncodeToString(digest[:]), nil
}

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
	BlockerParticles  = "512"
)

// lost marks a hand the tracker could not follow; the heuristic plays it.
const lost = -1

// Bot is one match's worth of state.
type Bot struct {
	Table    *table.Table
	tree     *cfr.Tree
	player   cfr.Model
	rng      *rand.Rand
	blockers *blockerRiver

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

type blockerRiver struct {
	model     *cfr.Empirical
	rng       *rand.Rand
	particles int
	known     cards.Set
	history   []cfr.OpponentObservation
	complete  bool
	dealt     bool
	solves    uint64
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
	return newModelSeeds(m, cfr.BuildTree(), rand.Uint64(), rand.Uint64())
}

// NewModelSeed plays a shared immutable model with a reproducible sampling
// stream for offline simulation branches.
func NewModelSeed(m cfr.Model, seed uint64) *Bot {
	return newModelSeeds(m, seededModelTree, seed, seed^0xD1B54A32D192ED03)
}

var seededModelTree = cfr.BuildTree()

func newModelSeeds(m cfr.Model, tree *cfr.Tree, seedA, seedB uint64) *Bot {
	return &Bot{
		Table:  table.New(),
		tree:   tree,
		player: m,
		rng:    rand.New(rand.NewPCG(seedA, seedB)),
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
	m, err := LoadSpinelModel()
	if err != nil {
		return nil, err
	}
	return NewModel(m), nil
}

// LoadSpinelModel decodes the frozen fitted policy once so offline six-seat
// simulations can share it across deterministic per-seat trackers.
func LoadSpinelModel() (cfr.Model, error) {
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
	return m, nil
}

// NewBlockerRiverResponse keeps the embedded Tourmaline policy for every
// ordinary decision and solves valid final-street betting nodes after
// conditioning its empirical base on hero's cards, muck, and public history.
func NewBlockerRiverResponse() (*Bot, error) {
	b, err := NewRiverResponse()
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(BlockerParticles)
	if err != nil || n < 64 || n > 8192 {
		return nil, fmt.Errorf("lapis: invalid blocker particle count %q", BlockerParticles)
	}
	response, ok := b.player.(*cfr.RiverResponse)
	if !ok {
		return nil, fmt.Errorf("lapis: blocker response has incompatible policy")
	}
	model, ok := response.EmpiricalBase()
	if !ok {
		return nil, fmt.Errorf("lapis: blocker response has no empirical base")
	}
	model, err = model.WithMemoBits(18)
	if err != nil {
		return nil, fmt.Errorf("lapis: blocker response cache: %w", err)
	}
	b.blockers = &blockerRiver{
		model: model, particles: n, complete: true,
		rng: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
	}
	return b, nil
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
	if b.blockers != nil {
		b.blockers.reset()
	}
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
	if b.blockers != nil {
		b.blockers.observe(b, event)
	}
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

func (s *blockerRiver) reset() {
	s.known = 0
	s.history = s.history[:0]
	s.complete = true
	s.dealt = false
	s.solves = 0
}

func (s *blockerRiver) observe(b *Bot, event wire.Event) {
	if !s.complete || b.node == lost || b.Table.Hand.Seat < 0 || b.Table.Hand.Seat > 1 {
		return
	}
	hero := b.Table.Hand.Seat
	switch event.Kind {
	case wire.EventDealHole:
		if event.Seat != hero {
			return
		}
		if s.dealt || len(b.Table.Hand.Cards) != 0 || len(event.Cards) != 5 || cards.NewSet(event.Cards).Len() != 5 {
			s.complete = false
			return
		}
		s.known |= cards.NewSet(event.Cards)
		s.dealt = true

	case wire.EventActed:
		n := &b.tree.Nodes[b.node]
		action := actionOf(event.Action.Kind)
		if n.Kind != cfr.KindBet || int(n.Actor) != event.Seat || !nodeHasAction(n, action) {
			s.complete = false
			return
		}
		if event.Seat != hero {
			s.history = append(s.history, cfr.OpponentObservation{View: b.view(n, 0), Action: action})
		}

	case wire.EventDrawResult:
		n := &b.tree.Nodes[b.node]
		if n.Kind != cfr.KindDraw || int(n.Actor) != event.Seat || event.Count < 0 || event.Count > 5 {
			s.complete = false
			return
		}
		if event.Seat != hero {
			s.history = append(s.history, cfr.OpponentObservation{View: b.view(n, 0), Draw: true, Action: event.Count})
			return
		}
		discarded := cards.NewSet(event.Discarded)
		drawn := cards.NewSet(event.Drawn)
		held := cards.NewSet(b.Table.Hand.Cards)
		if len(b.Table.Hand.Cards) != 5 || held.Len() != 5 || len(event.Discarded) != event.Count || len(event.Drawn) != event.Count ||
			discarded.Len() != len(event.Discarded) || drawn.Len() != len(event.Drawn) || discarded&^held != 0 || drawn&s.known != 0 {
			s.complete = false
			return
		}
		s.known |= discarded | drawn
	}
}

func nodeHasAction(n *cfr.Node, action int) bool {
	for _, candidate := range n.Acts {
		if int(candidate) == action {
			return true
		}
	}
	return false
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

// ProjectedDraw asks this bot's loaded Spinel policy at a valid heads-up draw
// node selected from a multi-seat public history. Betting fields intentionally
// stay zero at draw nodes, matching the original tracker.
func (b *Bot) ProjectedDraw(nodeID int32, seat int, hand []cards.Card, drawn [2]cfr.DrawCounts, lastAggressor int) (wire.Action, bool) {
	if len(hand) != deuceHandSize || cards.NewSet(hand).Len() != deuceHandSize || nodeID < 0 || int(nodeID) >= len(b.tree.Nodes) {
		return wire.Action{}, false
	}
	node := &b.tree.Nodes[nodeID]
	if node.Kind != cfr.KindDraw || int(node.Actor) != seat || seat < cfr.Btn || seat > cfr.BB {
		return wire.Action{}, false
	}
	sorted := cards.SortedByRank(hand)
	view := cfr.View{Seat: seat, Node: nodeID, Street: int(node.Street), Drawn: drawn, LastAggr: lastAggressor, Rand: b.rng.Float64()}
	copy(view.Hand[:], sorted)
	keep, ok := b.player.Draw(&view)
	if !ok || keep&^uint8(31) != 0 {
		return wire.Action{}, false
	}
	discards := make([]cards.Card, 0, deuceHandSize)
	for i, card := range sorted {
		if keep&(1<<i) == 0 {
			discards = append(discards, card)
		}
	}
	return wire.Discard(discards), true
}

const deuceHandSize = 5

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
	view := b.view(node, 0)
	copy(view.Hand[:], sorted)
	if hand.Seat == cfr.Btn {
		view.FixedGroup = b.fixed.Group()
	}

	switch decision.Kind {
	case wire.DecisionWager:
		if node.Kind != cfr.KindBet || node.Facing != (decision.Call != nil) {
			return wire.Action{}, false
		}
		if b.blockers != nil {
			if action, ok := b.blockers.decide(b, &view, decision); ok {
				return action, true
			}
		}
		view.Rand = b.rng.Float64()
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
		view.Rand = b.rng.Float64()
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

func (b *Bot) view(node *cfr.Node, random float64) cfr.View {
	seat := int(node.Actor)
	v := cfr.View{Seat: seat, Node: b.node, Street: int(node.Street), Drawn: b.drawn,
		LastAggr: b.lastAggr, Rand: random}
	if node.Kind == cfr.KindBet {
		v.Pot = node.Commit[0] + node.Commit[1]
		v.ToCall = max(0, node.Commit[1-seat]-node.Commit[seat])
		v.Facing = node.Facing
		v.Wagers = int(node.Wagers)
		v.CanRaise = node.Acts[len(node.Acts)-1] == cfr.Aggr
	}
	return v
}

func (s *blockerRiver) decide(b *Bot, view *cfr.View, decision wire.Decision) (wire.Action, bool) {
	if !s.complete || !s.dealt || len(b.Table.Hand.Cards) != 5 || decision.Kind != wire.DecisionWager {
		return wire.Action{}, false
	}
	n := &b.tree.Nodes[b.node]
	if n.Kind != cfr.KindBet || n.Street != cfr.Draw3 || int(n.Actor) != b.Table.Hand.Seat || n.Facing != (decision.Call != nil) {
		return wire.Action{}, false
	}
	known := s.known | cards.NewSet(b.Table.Hand.Cards)
	belief := cfr.OpponentBelief(s.model, s.history, known, s.rng, s.particles)
	action, _, ok := cfr.BestRiverAction(b.tree, b.node, *view, belief, s.model)
	if !ok {
		return wire.Action{}, false
	}
	s.solves++
	switch action {
	case cfr.Fold:
		return wire.Fold(), true
	case cfr.Aggr:
		return wire.Raise(0), true
	default:
		if n.Facing {
			return wire.Call(), true
		}
		return wire.Check(), true
	}
}
