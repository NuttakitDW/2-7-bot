package cfr

import (
	"fmt"
	"math"
)

// PolicyBoost is an additive logit model. Each scalar tree contributes to one
// output class; softmax is applied once after all trees and the bias are summed.
// Binary classifiers use zero logits for their first class and fitted logits
// for their second. Classes absent during fitting retain zero probability.
type PolicyBoost struct {
	Classes []int       `json:"classes"`
	Bias    [6]float64  `json:"bias"`
	Trees   []BoostTree `json:"trees"`
}

type BoostTree struct {
	Class int         `json:"class"`
	Nodes []BoostNode `json:"nodes"`
}

type BoostNode struct {
	Feature   int     `json:"feature"`
	Threshold float64 `json:"threshold"`
	Left      int     `json:"left"`
	Right     int     `json:"right"`
	Value     float64 `json:"value"`
}

func (b *PolicyBoost) validate(classCount int) error {
	if b == nil || len(b.Classes) == 0 || len(b.Trees) == 0 || len(b.Trees) > 10000 {
		return fmt.Errorf("empty or oversized boosted model")
	}
	var present [6]bool
	for _, c := range b.Classes {
		if c < 0 || c >= classCount || present[c] {
			return fmt.Errorf("invalid boosted class %d", c)
		}
		present[c] = true
	}
	// These bounds leave ample room for fitted logits while ensuring that even
	// every tree's largest leaf plus its bias can be summed without overflow.
	validScore := func(v float64) bool { return !math.IsNaN(v) && math.Abs(v) <= 1e6 }
	for c, v := range b.Bias {
		if !validScore(v) || (!present[c] && v != 0) {
			return fmt.Errorf("invalid boosted bias")
		}
	}
	for _, tree := range b.Trees {
		if tree.Class < 0 || tree.Class >= classCount || !present[tree.Class] || len(tree.Nodes) == 0 {
			return fmt.Errorf("invalid boosted tree")
		}
		for i, n := range tree.Nodes {
			if math.IsNaN(n.Threshold) || math.IsInf(n.Threshold, 0) || !validScore(n.Value) {
				return fmt.Errorf("nonfinite or excessive boosted node value")
			}
			if n.Feature == -1 {
				continue
			}
			if n.Feature < 0 || n.Feature >= PolicyFeatureCount || n.Left <= i || n.Right <= i || n.Left >= len(tree.Nodes) || n.Right >= len(tree.Nodes) {
				return fmt.Errorf("invalid boosted branch %d", i)
			}
		}
	}
	return nil
}

func (b *PolicyBoost) predict(x *[PolicyFeatureCount]float64) [6]float64 {
	scores := b.Bias
	for _, tree := range b.Trees {
		i := 0
		for tree.Nodes[i].Feature >= 0 {
			n := &tree.Nodes[i]
			if x[n.Feature] <= n.Threshold {
				i = n.Left
			} else {
				i = n.Right
			}
		}
		scores[tree.Class] += tree.Nodes[i].Value
	}
	maximum := math.Inf(-1)
	for _, c := range b.Classes {
		maximum = math.Max(maximum, scores[c])
	}
	var out [6]float64
	total := 0.0
	for _, c := range b.Classes {
		out[c] = math.Exp(scores[c] - maximum)
		total += out[c]
	}
	for _, c := range b.Classes {
		out[c] /= total
	}
	return out
}

func (m *Empirical) validateBoosts() error {
	for i, b := range m.BettingBoost {
		if len(m.Betting[i]) != 0 || len(m.BettingForest[i]) != 0 {
			return fmt.Errorf("boosted model contains betting trees of another format")
		}
		if err := b.validate(3); err != nil {
			return fmt.Errorf("betting boost %d: %w", i, err)
		}
	}
	for i, b := range m.DrawingBoost {
		if len(m.Drawing[i]) != 0 || len(m.DrawingForest[i]) != 0 {
			return fmt.Errorf("boosted model contains drawing trees of another format")
		}
		if err := b.validate(6); err != nil {
			return fmt.Errorf("drawing boost %d: %w", i, err)
		}
	}
	return nil
}
