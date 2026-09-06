package cfr

import (
	"encoding/json"
	"fmt"
	"math"

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
}

type PolicyNode struct {
	Feature   int        `json:"feature"` // -1 at a leaf
	Threshold float64    `json:"threshold"`
	Left      int        `json:"left"`
	Right     int        `json:"right"`
	Prob      [6]float64 `json:"prob"`
}

const PolicyFeatureCount = 56

// PolicyFeatures version 2 adds pot size, call size and pot odds at 29..31.
// Version 5 adds prior public actions at 32..55, six counts per street:
// own/opponent raises, own/opponent checks, own/opponent calls.
// Card ranks use deuce=2 through ace=14; sizes are in big bets.
func PolicyFeatures(v *View) [PolicyFeatureCount]float64 {
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
			}
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
	if m.Version < 1 || m.Version > 5 {
		return nil, fmt.Errorf("unsupported empirical model version %d", m.Version)
	}
	featureCount := PolicyFeatureCount
	if m.Version == 1 {
		featureCount = 29
	} else if m.Version < 5 {
		featureCount = 32
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

func predictPolicy(nodes []PolicyNode, x [PolicyFeatureCount]float64) [6]float64 {
	i := 0
	for nodes[i].Feature >= 0 {
		n := nodes[i]
		if x[n.Feature] <= n.Threshold {
			i = n.Left
		} else {
			i = n.Right
		}
	}
	return nodes[i].Prob
}

func predictForest(forest [][]PolicyNode, x [PolicyFeatureCount]float64) [6]float64 {
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
	x := PolicyFeatures(v)
	if m.Version >= 4 {
		return predictForest(m.BettingForest[v.Street], x)
	}
	return predictPolicy(m.Betting[v.Street], x)
}

func (m *Empirical) predictDraw(v *View) [6]float64 {
	x := PolicyFeatures(v)
	if m.Version >= 4 {
		return predictForest(m.DrawingForest[v.Street-1], x)
	}
	return predictPolicy(m.Drawing[v.Street-1], x)
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
