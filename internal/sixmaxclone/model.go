// Package sixmaxclone provides a compact, public-prefix-only predraw policy
// model. Offline training is kept in cmd/sixmaxclonefit.
package sixmaxclone

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/policy"
)

const SchemaVersion = 1

const maxTreeDepth = 4

type Action uint8

const (
	Fold Action = iota
	Passive
	Aggressive
)

type Snapshot struct {
	Hand                          []cards.Card
	Position, ActivePlayers       int
	Aggressions, Calls, OwnRaises int
	Pot, Call, SmallBet           uint64
}

type Features struct {
	Category, KeepCount, HighestKeep, SecondHighestKeep, KeepRankSum, MadeSecondHighest int
	HasDeuce, PairCount, StraightRisk                                                   int
	Position, ActivePlayers                                                             int
	Aggressions, Calls, OwnRaises                                                       int
	PotSmallBets, CallSmallBets                                                         int
	Facing                                                                              bool
}

var bettingOrderPosition = [...]int{3, 4, 5, 0, 1, 2}

func BuildFeatures(s Snapshot) (Features, error) {
	if len(s.Hand) != deuce.HandSize || cards.NewSet(s.Hand).Len() != deuce.HandSize || s.SmallBet == 0 ||
		s.Position < 0 || s.Position >= 6 || s.ActivePlayers < 2 || s.ActivePlayers > 6 ||
		s.Aggressions < 0 || s.Calls < 0 || s.OwnRaises < 0 {
		return Features{}, fmt.Errorf("invalid predraw snapshot")
	}
	potSmallBets, callSmallBets := s.Pot/s.SmallBet, s.Call/s.SmallBet
	maxInt := uint64(^uint(0) >> 1)
	if potSmallBets > maxInt || callSmallBets > maxInt {
		return Features{}, fmt.Errorf("predraw snapshot ratio overflows int")
	}
	keep := policy.DrawingKeep(s.Hand)
	distinct := cards.DistinctRanks(s.Hand)
	f := Features{Category: int(deuce.Categorize(s.Hand)), KeepCount: len(keep), PairCount: len(s.Hand) - len(distinct),
		Position: bettingOrderPosition[s.Position], ActivePlayers: s.ActivePlayers, Aggressions: s.Aggressions, Calls: s.Calls, OwnRaises: s.OwnRaises,
		PotSmallBets: int(potSmallBets), CallSmallBets: int(callSmallBets), Facing: s.Call > 0}
	if len(keep) > 0 {
		f.HighestKeep = int(keep[len(keep)-1])
		for _, rank := range keep {
			f.KeepRankSum += int(rank)
			if rank == cards.Two {
				f.HasDeuce = 1
			}
		}
		if len(keep) >= 2 {
			f.SecondHighestKeep = int(keep[len(keep)-2])
		}
	}
	if len(distinct) >= 2 && deuce.Categorize(s.Hand) >= deuce.Ten {
		f.MadeSecondHighest = int(distinct[len(distinct)-2])
	}
	for i := 0; i+3 < len(distinct); i++ {
		if int(distinct[i+3])-int(distinct[i]) <= 4 {
			f.StraightRisk = 1
			break
		}
	}
	return f, nil
}

const (
	FeatureCategory = iota
	FeatureKeepCount
	FeatureHighestKeep
	FeatureSecondHighestKeep
	FeatureKeepRankSum
	FeatureMadeSecondHighest
	FeatureHasDeuce
	FeaturePairCount
	FeatureStraightRisk
	FeaturePosition
	FeatureActivePlayers
	FeatureAggressions
	FeatureCalls
	FeatureOwnRaises
	FeaturePotSmallBets
	FeatureCallSmallBets
	featureCount
)

const FeatureCount = featureCount

type Leaf struct {
	Groups        float64    `json:"groups"`
	Probabilities [3]float64 `json:"probabilities"`
}
type Node struct {
	Feature   int     `json:"feature,omitempty"`
	Left      int     `json:"left,omitempty"`
	Right     int     `json:"right,omitempty"`
	Threshold float64 `json:"threshold,omitempty"`
	Leaf      *Leaf   `json:"leaf,omitempty"`
}
type Tree struct {
	Nodes []Node `json:"nodes"`
}
type Model struct {
	Schema        int             `json:"schema"`
	Source        string          `json:"source"`
	MinGroups     float64         `json:"minGroups"`
	MinConfidence float64         `json:"minConfidence"`
	MinMargin     float64         `json:"minMargin"`
	Trees         map[string]Tree `json:"trees"`
}

func Decode(data []byte) (*Model, error) {
	var m Model
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m Model) Validate() error {
	if m.Schema != SchemaVersion || m.MinGroups < 20 || m.MinConfidence < 0 || m.MinConfidence > 1 || m.MinMargin < 0 || m.MinMargin > 1 ||
		math.IsNaN(m.MinGroups+m.MinConfidence+m.MinMargin) {
		return fmt.Errorf("invalid model metadata")
	}
	for _, context := range []string{"facing", "checked"} {
		tree, ok := m.Trees[context]
		if !ok || len(tree.Nodes) == 0 {
			return fmt.Errorf("missing %s tree", context)
		}
		for i, node := range tree.Nodes {
			if node.Leaf != nil {
				if node.Leaf.Groups < 0 || math.IsNaN(node.Leaf.Groups) || math.IsInf(node.Leaf.Groups, 0) {
					return fmt.Errorf("invalid leaf groups")
				}
				sum := 0.0
				for _, p := range node.Leaf.Probabilities {
					if p < 0 || math.IsNaN(p) || math.IsInf(p, 0) {
						return fmt.Errorf("invalid leaf")
					}
					sum += p
				}
				if math.Abs(sum-1) > 1e-6 {
					return fmt.Errorf("invalid probability sum")
				}
				continue
			}
			if node.Feature < 0 || node.Feature >= featureCount || math.IsNaN(node.Threshold) || math.IsInf(node.Threshold, 0) || node.Left <= i || node.Right <= i || node.Left >= len(tree.Nodes) || node.Right >= len(tree.Nodes) {
				return fmt.Errorf("invalid tree node")
			}
		}
		var validateDepth func(int, int) error
		validateDepth = func(index, depth int) error {
			if depth > maxTreeDepth {
				return fmt.Errorf("tree depth exceeds %d", maxTreeDepth)
			}
			node := tree.Nodes[index]
			if node.Leaf != nil {
				return nil
			}
			if err := validateDepth(node.Left, depth+1); err != nil {
				return err
			}
			return validateDepth(node.Right, depth+1)
		}
		if err := validateDepth(0, 0); err != nil {
			return fmt.Errorf("invalid %s tree: %w", context, err)
		}
	}
	return nil
}

func (m Model) Predict(f Features) (Action, bool) {
	context := "checked"
	if f.Facing {
		context = "facing"
	}
	tree, ok := m.Trees[context]
	if !ok || len(tree.Nodes) == 0 {
		return 0, false
	}
	i := 0
	for steps := 0; steps <= len(tree.Nodes); steps++ {
		n := tree.Nodes[i]
		if n.Leaf != nil {
			if n.Leaf.Groups < m.MinGroups {
				return 0, false
			}
			best, second, action := -1.0, -1.0, Fold
			for j, p := range n.Leaf.Probabilities {
				if p > best {
					second, best, action = best, p, Action(j)
				} else if p > second {
					second = p
				}
			}
			if best < m.MinConfidence || best-second < m.MinMargin {
				return 0, false
			}
			return action, true
		}
		if f.Value(n.Feature) <= n.Threshold {
			i = n.Left
		} else {
			i = n.Right
		}
	}
	return 0, false
}

func (f Features) Value(feature int) float64 {
	values := [...]int{f.Category, f.KeepCount, f.HighestKeep, f.SecondHighestKeep, f.KeepRankSum, f.MadeSecondHighest, f.HasDeuce, f.PairCount, f.StraightRisk, f.Position, f.ActivePlayers,
		f.Aggressions, f.Calls, f.OwnRaises, f.PotSmallBets, f.CallSmallBets}
	return float64(values[feature])
}
