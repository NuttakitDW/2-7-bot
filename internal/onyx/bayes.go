package onyx

import (
	_ "embed"
	"fmt"
	"math/rand/v2"
	"strconv"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

//go:embed opponent_policy.json
var opponentPolicyData []byte

var beliefParticleCount = "512"

// riverResponseAlpha sharpens the fitted opponent's predicted river
// responses toward its most likely action (cfr.Empirical.ResponseAlpha).
var riverResponseAlpha = "1"

type riverSolver struct {
	model       *cfr.Empirical
	riverRange  *riverRangeModel
	equilibrium *riverEquilibrium
	priors      *riverPriors
	planBelief  []cfr.BeliefHand
	planKnown   cards.Set
	planCount   int
	tree        *cfr.Tree
	node        int32
	drawn       [2]cfr.DrawCounts
	lastAggr    int
	known       cards.Set
	history     []cfr.OpponentObservation
	rng         *rand.Rand
	particles   int
	fixed       cfr.FixedCard
}

func newRiverSolver() (*riverSolver, error) {
	n, err := strconv.Atoi(beliefParticleCount)
	if err != nil || n < 64 || n > 8192 {
		return nil, fmt.Errorf("onyx: invalid belief particle count %q", beliefParticleCount)
	}
	m, err := decodeOpponentModel(opponentPolicyData)
	if err != nil {
		return nil, fmt.Errorf("onyx: opponent model: %w", err)
	}
	alpha, err := strconv.ParseFloat(riverResponseAlpha, 64)
	if err != nil || alpha < 1 || alpha > 16 {
		return nil, fmt.Errorf("onyx: invalid river response sharpening %q", riverResponseAlpha)
	}
	m.ResponseAlpha = alpha
	var ranges *riverRangeModel
	if modelSelection == "range-call" {
		ranges, err = decodeRiverRange(opponentPolicyData)
		if err != nil {
			return nil, fmt.Errorf("onyx: river range: %w", err)
		}
	}
	tree := cfr.BuildTree()
	var equilibrium *riverEquilibrium
	if modelSelection == "nash-river" {
		equilibrium, err = decodeRiverEquilibrium(opponentPolicyData, tree)
		if err != nil {
			return nil, fmt.Errorf("onyx: river equilibrium: %w", err)
		}
	}
	var priors *riverPriors
	if modelSelection == "range-response" || modelSelection == "all-response" {
		priors, err = decodeRiverPriors(opponentPolicyData)
		if err != nil {
			return nil, fmt.Errorf("onyx: river priors: %w", err)
		}
	}
	return &riverSolver{model: m, riverRange: ranges, equilibrium: equilibrium, priors: priors, tree: tree, node: -1, particles: n, rng: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))}, nil
}

func (s *riverSolver) reset() {
	s.node = s.tree.Root
	s.lastAggr = -1
	s.known = 0
	s.history = s.history[:0]
	s.planBelief = nil
	s.planKnown, s.planCount = 0, 0
	for p := range s.drawn {
		for street := range s.drawn[p] {
			s.drawn[p][street] = -1
		}
	}
}

func (s *riverSolver) view(n *cfr.Node) cfr.View {
	v := cfr.View{Node: s.node, Seat: int(n.Actor), Street: int(n.Street), Facing: n.Facing, Wagers: int(n.Wagers), Drawn: s.drawn, LastAggr: s.lastAggr}
	if n.Kind == cfr.KindBet {
		v.Pot = n.Commit[0] + n.Commit[1]
		v.ToCall = max(0, n.Commit[1-v.Seat]-n.Commit[v.Seat])
		v.CanRaise = n.Acts[len(n.Acts)-1] == cfr.Aggr
	}
	return v
}

func (s *riverSolver) observe(e wire.Event, hero int) {
	if s.node < 0 {
		return
	}
	if e.Kind == wire.EventHandStart && e.Button != cfr.Btn {
		s.node = -1
		return
	}
	if e.Kind == wire.EventDealHole && e.Seat == hero {
		s.known |= cards.NewSet(e.Cards)
		if hero != cfr.Btn {
			s.fixed.ObserveBigBlind(cards.NewSet(e.Cards))
		}
	}
	if e.Kind != wire.EventActed && e.Kind != wire.EventDrawResult {
		return
	}
	n := &s.tree.Nodes[s.node]
	if e.Seat < 0 || e.Seat > 1 || int(n.Actor) != e.Seat {
		s.node = -1
		return
	}
	v := s.view(n)
	if e.Kind == wire.EventDrawResult {
		if n.Kind != cfr.KindDraw || e.Count < 0 || e.Count > 5 {
			s.node = -1
			return
		}
		if e.Seat == hero {
			s.known |= cards.NewSet(e.Drawn) | cards.NewSet(e.Discarded)
		} else {
			s.history = append(s.history, cfr.OpponentObservation{View: v, Draw: true, Action: e.Count})
		}
		s.drawn[e.Seat][n.Street] = int8(e.Count)
		s.node = n.Next[0]
		return
	}
	if n.Kind != cfr.KindBet {
		s.node = -1
		return
	}
	a := -1
	switch e.Action.Kind {
	case wire.ActionFold:
		a = cfr.Fold
	case wire.ActionCheck, wire.ActionCall:
		a = cfr.Pass
	case wire.ActionBet, wire.ActionRaise:
		a = cfr.Aggr
	}
	found := false
	for i, action := range n.Acts {
		if int(action) == a {
			s.node = n.Next[i]
			found = true
			break
		}
	}
	if !found {
		s.node = -1
		return
	}
	if e.Seat != hero {
		s.history = append(s.history, cfr.OpponentObservation{View: v, Action: a})
	}
	if a == cfr.Aggr {
		s.lastAggr = e.Seat
	}
}

func (s *riverSolver) decide(hero int, hand []cards.Card, d wire.Decision) (wire.Action, bool) {
	if s.node < 0 || d.Kind != wire.DecisionWager || len(hand) != 5 {
		return wire.Action{}, false
	}
	n := &s.tree.Nodes[s.node]
	if n.Kind != cfr.KindBet || n.Street != cfr.Draw3 || int(n.Actor) != hero || n.Facing != (d.Call != nil) {
		return wire.Action{}, false
	}
	v := s.view(n)
	copy(v.Hand[:], cards.SortedByRank(hand))
	belief := s.belief(hero, s.known|cards.NewSet(hand))
	a, _, ok := cfr.BestRiverAction(s.tree, s.node, v, belief, s.model)
	if !ok {
		return wire.Action{}, false
	}
	switch a {
	case cfr.Fold:
		return wire.Fold(), true
	case cfr.Aggr:
		return wire.Raise(0), true
	default:
		if d.Call != nil {
			return wire.Call(), true
		}
		return wire.Check(), true
	}
}

// belief is the opponent's hand distribution given everything public,
// including the arena's constant big-blind card once it is identified.
func (s *riverSolver) belief(hero int, known cards.Set) []cfr.BeliefHand {
	held := cards.Set(0)
	if hero == cfr.Btn {
		held = s.fixed.OpponentHolds() &^ known
	}
	return cfr.OpponentBeliefHolding(s.model, s.history, known, held, s.rng, s.particles)
}
