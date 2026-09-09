package cfr

import (
	"encoding/json"
	"fmt"
	"math"
	"sync/atomic"
	"unsafe"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/policy"
)

// Empirical is an offline opponent model, not the trained response strategy.
// Its features contain only the acting player's cards and public information.
type Empirical struct {
	Version int             `json:"version"`
	Betting [4][]PolicyNode `json:"betting"`
	Drawing [3][]PolicyNode `json:"drawing"`
	// Versions 4 and 5 average leaf distributions from multiple fitted trees.
	BettingForest [4][][]PolicyNode `json:"betting_forest,omitempty"`
	DrawingForest [3][][]PolicyNode `json:"drawing_forest,omitempty"`
	// Version 8 uses additive logits instead of averaged leaf probabilities.
	BettingBoost [4]*PolicyBoost `json:"betting_boost,omitempty"`
	DrawingBoost [3]*PolicyBoost `json:"drawing_boost,omitempty"`

	// ResponseAlpha sharpens river response predictions (responseProbabilities).
	ResponseAlpha float64 `json:"-"`

	memo     *policyMemo
	compiled *compiledForests
}

type compiledForests struct {
	betting [4][]compactTree
	drawing [3][]compactTree
}

// compile flattens the fitted forests for prediction. Decoding does it
// once; a model built in memory gets it on first use.
func (m *Empirical) compile() *compiledForests {
	if c := (*compiledForests)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&m.compiled)))); c != nil {
		return c
	}
	c := &compiledForests{}
	for i := range m.BettingForest {
		c.betting[i] = compileForest(m.BettingForest[i])
	}
	for i := range m.DrawingForest {
		c.drawing[i] = compileForest(m.DrawingForest[i])
	}
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&m.compiled)), unsafe.Pointer(c))
	return c
}

type PolicyNode struct {
	Feature   int        `json:"feature"` // -1 at a leaf
	Threshold float64    `json:"threshold"`
	Left      int        `json:"left"`
	Right     int        `json:"right"`
	Prob      [6]float64 `json:"prob"`
}

const PolicyFeatureCount = 64

// PolicyFeatures version 2 adds pot size, call size and pot odds at 29..31.
// Version 5 adds prior public actions at 32..55, six counts per street:
// own/opponent raises, own/opponent checks, own/opponent calls.
// Version 6 adds at 56..59 the public node (numbered depth-first, so a
// threshold on it isolates a subtree, which is a history prefix), the
// draw-count difference on the latest draw, total opponent wagers so far,
// and whether the acting player made the hand's last wager.
// Version 7 adds at 60..63 static-opponent equity with zero through three
// future draws. These describe private drawing potential, not opponent ranges.
// Card ranks use deuce=2 through ace=14; sizes are in big bets.
func PolicyFeatures(v *View) [PolicyFeatureCount]float64 {
	x := policyFeatures(v)
	equity := policyEquities()[handclass.Of(v.Hand[:])]
	copy(x[60:], equity[:])
	return x
}

// Older model versions retain their original inputs and avoid constructing
// the equity table. The public exporter includes every current feature.
func policyFeatures(v *View) [PolicyFeatureCount]float64 {
	x := policyPrivateFeatures[handclass.Of(v.Hand[:])]
	x[0], x[1], x[2] = float64(v.Seat), float64(v.Wagers), float64(AggrState(v.Seat, v.LastAggr))
	if v.Facing {
		x[3] = 1
	}
	if v.CanRaise {
		x[4] = 1
	}
	for s := 1; s <= 3; s++ {
		x[3+2*s] = float64(v.Drawn[v.Seat][s])
		x[4+2*s] = float64(v.Drawn[1-v.Seat][s])
	}
	x[28] = float64(v.Street)
	x[29] = float64(v.Pot) / BigBet
	x[30] = float64(v.ToCall) / BigBet
	if v.Pot+v.ToCall > 0 {
		x[31] = float64(v.ToCall) / float64(v.Pot+v.ToCall)
	}
	if v.Node >= 0 && int(v.Node) < len(policyHistories) {
		history := &policyHistories[v.Node]
		for street := 0; street < Streets; street++ {
			for action := 0; action < 3; action++ {
				x[32+street*6+action*2] = float64(history[street][v.Seat][action])
				x[33+street*6+action*2] = float64(history[street][1-v.Seat][action])
				if action == 0 {
					x[58] += float64(history[street][1-v.Seat][action])
				}
			}
		}
		x[56] = float64(v.Node)
		if v.Street > Predraw {
			x[57] = float64(v.Drawn[v.Seat][v.Street] - v.Drawn[1-v.Seat][v.Street])
		}
		if v.LastAggr == v.Seat {
			x[59] = 1
		}
	}
	return x
}

// Private features depend only on rank multiset and flushness. Cache them
// once; the model and particle filter query these facts many times per hand.
var policyPrivateFeatures = func() [handclass.Num][PolicyFeatureCount]float64 {
	var out [handclass.Num][PolicyFeatureCount]float64
	for id := range out {
		if handclass.Weight(handclass.ID(id)) != 0 {
			out[id] = privatePolicyFeatures(cards.SortedByRank(handclass.Representative(handclass.ID(id))))
		}
	}
	return out
}()

func privatePolicyFeatures(hand []cards.Card) [PolicyFeatureCount]float64 {
	var x [PolicyFeatureCount]float64
	value := deuce.Eval(hand)
	x[11] = float64(value)
	x[12] = float64(value.Class())
	for i, c := range hand {
		x[13+i] = float64(c.Rank)
	}
	keep := policy.DrawingKeep(hand)
	x[18] = float64(len(keep))
	for i, r := range keep {
		x[19+i] = float64(r)
	}
	for _, c := range hand {
		if c.Rank <= cards.Seven {
			x[24]++
		}
		if c.Rank <= cards.Nine {
			x[25]++
		}
	}
	x[26] = float64(len(cards.DistinctRanks(hand)))
	if cards.SameSuit(hand) {
		x[27] = 1
	}
	return x
}

func DecodeEmpirical(raw []byte) (*Empirical, error) {
	var m Empirical
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m.Version < 1 || m.Version > 8 {
		return nil, fmt.Errorf("unsupported empirical model version %d", m.Version)
	}
	if m.Version == 8 {
		if err := m.validateBoosts(); err != nil {
			return nil, err
		}
		return &m, nil
	}
	for _, b := range m.BettingBoost {
		if b != nil {
			return nil, fmt.Errorf("older model contains a betting boost")
		}
	}
	for _, b := range m.DrawingBoost {
		if b != nil {
			return nil, fmt.Errorf("older model contains a drawing boost")
		}
	}
	featureCount := PolicyFeatureCount
	if m.Version == 1 {
		featureCount = 29
	} else if m.Version < 5 {
		featureCount = 32
	} else if m.Version == 5 {
		featureCount = 56
	} else if m.Version == 6 {
		featureCount = 60
	}
	validate := func(nodes []PolicyNode) error {
		if len(nodes) == 0 {
			return fmt.Errorf("empty policy tree")
		}
		for i, n := range nodes {
			if math.IsNaN(n.Threshold) || math.IsInf(n.Threshold, 0) {
				return fmt.Errorf("nonfinite threshold")
			}
			if n.Feature == -1 {
				total := 0.0
				for _, p := range n.Prob {
					if p < 0 || math.IsNaN(p) || math.IsInf(p, 0) {
						return fmt.Errorf("invalid probability")
					}
					total += p
				}
				if total <= 0 || math.IsInf(total, 0) {
					return fmt.Errorf("empty leaf")
				}
			} else if n.Feature < 0 || n.Feature >= featureCount || n.Left <= i || n.Right <= i || n.Left >= len(nodes) || n.Right >= len(nodes) {
				return fmt.Errorf("invalid policy branch %d", i)
			}
		}
		return nil
	}
	if m.Version >= 4 {
		for _, nodes := range m.Betting {
			if len(nodes) != 0 {
				return nil, fmt.Errorf("forest model contains a legacy betting tree")
			}
		}
		for _, nodes := range m.Drawing {
			if len(nodes) != 0 {
				return nil, fmt.Errorf("forest model contains a legacy drawing tree")
			}
		}
		forests := append(m.BettingForest[:], m.DrawingForest[:]...)
		for _, forest := range forests {
			if len(forest) == 0 {
				return nil, fmt.Errorf("empty policy forest")
			}
			for _, nodes := range forest {
				if err := validate(nodes); err != nil {
					return nil, err
				}
			}
		}
		m.compile()
		return &m, nil
	}
	for _, forest := range m.BettingForest {
		if len(forest) != 0 {
			return nil, fmt.Errorf("legacy model contains a betting forest")
		}
	}
	for _, forest := range m.DrawingForest {
		if len(forest) != 0 {
			return nil, fmt.Errorf("legacy model contains a drawing forest")
		}
	}
	for _, nodes := range m.Betting {
		if err := validate(nodes); err != nil {
			return nil, err
		}
	}
	for _, nodes := range m.Drawing {
		if err := validate(nodes); err != nil {
			return nil, err
		}
	}
	return &m, nil
}

func predictPolicy(nodes []PolicyNode, x *[PolicyFeatureCount]float64) [6]float64 {
	i := 0
	for nodes[i].Feature >= 0 {
		n := &nodes[i]
		if x[n.Feature] <= n.Threshold {
			i = n.Left
		} else {
			i = n.Right
		}
	}
	return nodes[i].Prob
}

func predictForest(forest [][]PolicyNode, x *[PolicyFeatureCount]float64) [6]float64 {
	var out [6]float64
	for _, nodes := range forest {
		p := predictPolicy(nodes, x)
		total := 0.0
		for _, value := range p {
			total += value
		}
		for i, value := range p {
			out[i] += value / total
		}
	}
	for i := range out {
		out[i] /= float64(len(forest))
	}
	return out
}

func (m *Empirical) predictBet(v *View) [6]float64 {
	if m.memo != nil {
		return m.memo.lookup(v, false, m.computeBet)
	}
	return m.computeBet(v)
}

func (m *Empirical) computeBet(v *View) [6]float64 {
	x := m.features(v)
	if m.Version == 8 {
		return m.BettingBoost[v.Street].predict(&x)
	}
	if m.Version >= 4 {
		return predictCompact(m.compile().betting[v.Street], &x)
	}
	return predictPolicy(m.Betting[v.Street], &x)
}

func (m *Empirical) predictDraw(v *View) [6]float64 {
	if m.memo != nil {
		return m.memo.lookup(v, true, m.computeDraw)
	}
	return m.computeDraw(v)
}

func (m *Empirical) computeDraw(v *View) [6]float64 {
	x := m.features(v)
	if m.Version == 8 {
		return m.DrawingBoost[v.Street-1].predict(&x)
	}
	if m.Version >= 4 {
		return predictCompact(m.compile().drawing[v.Street-1], &x)
	}
	return predictPolicy(m.Drawing[v.Street-1], &x)
}

func (m *Empirical) features(v *View) [PolicyFeatureCount]float64 {
	if m.Version >= 7 {
		return PolicyFeatures(v)
	}
	return policyFeatures(v)
}

func samplePolicy(p [6]float64, n int, r float64) (int, bool) {
	total := 0.0
	for _, v := range p[:n] {
		total += v
	}
	if total <= 0 {
		return 0, false
	}
	r *= total
	for i, v := range p[:n] {
		r -= v
		if r < 0 {
			return i, true
		}
	}
	return n - 1, true
}

func (m *Empirical) Bet(v *View) (int, bool) {
	p := m.predictBet(v)
	if !v.Facing {
		p[Fold] = 0
	}
	if !v.CanRaise {
		p[Aggr] = 0
	}
	return samplePolicy(p, 3, v.Rand)
}

func (m *Empirical) Draw(v *View) (uint8, bool) {
	p := m.predictDraw(v)
	n, ok := samplePolicy(p, 6, v.Rand)
	if !ok {
		return 0, false
	}
	return m.keepForCount(v, n), true
}

// Version 3 fills an undersized structural keep with unused ranks before
// retaining pairs. Older models retain their original interpretation.
func (m *Empirical) keepForCount(v *View, n int) uint8 {
	if m.Version < 3 || n == 0 {
		return empiricalKeep(v, n)
	}
	want := 5 - n
	ranks := policy.DrawingKeep(v.Hand[:])
	if len(ranks) > want {
		ranks = ranks[:want]
	}
	mask := keepMask(v.Hand[:], ranks)
	count := len(ranks)
	var used [15]bool
	for _, rank := range ranks {
		used[rank] = true
	}
	for i, c := range v.Hand {
		if count == want {
			break
		}
		if !used[c.Rank] {
			mask |= 1 << i
			used[c.Rank] = true
			count++
		}
	}
	for i := range v.Hand {
		if count == want {
			break
		}
		if mask&(1<<i) == 0 {
			mask |= 1 << i
			count++
		}
	}
	return mask
}

func empiricalKeep(v *View, n int) uint8 {
	if n == 0 {
		return 31
	}
	// The model predicts draw count. Keep the structural low-card selection,
	// then fill with the lowest remaining cards when it asks to retain more.
	want := 5 - n
	ranks := policy.DrawingKeep(v.Hand[:])
	if len(ranks) > want {
		ranks = ranks[:want]
	}
	mask := keepMask(v.Hand[:], ranks)
	count := len(ranks)
	for i := range v.Hand {
		if count >= want {
			break
		}
		if mask&(1<<i) == 0 {
			mask |= 1 << i
			count++
		}
	}
	return mask
}
