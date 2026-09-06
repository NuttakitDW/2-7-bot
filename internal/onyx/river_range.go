package onyx

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/deuce"
)

// Each public-history leaf stores an empirical distribution of opponent
// showdown values. Current opponent cards are never inputs to this model.
type riverRangeNode struct {
	Feature     int             `json:"feature"`
	Threshold   float64         `json:"threshold"`
	Left        int             `json:"left"`
	Right       int             `json:"right"`
	Values      []weightedValue `json:"values,omitempty"`
	rangeValues handRange
}

type riverRangeModel struct {
	Margin float64            `json:"margin"`
	Forest [][]riverRangeNode `json:"forest"`
}

func decodeRiverRange(raw []byte) (*riverRangeModel, error) {
	raw, err := modelJSON(raw)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Model *riverRangeModel `json:"river_range"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	m := envelope.Model
	if m == nil || len(m.Forest) == 0 || len(m.Forest) > 128 || !(m.Margin >= 0 && m.Margin <= .25) {
		return nil, fmt.Errorf("invalid river range model")
	}
	for _, tree := range m.Forest {
		if len(tree) == 0 || len(tree) > 50000 {
			return nil, fmt.Errorf("invalid river range tree size")
		}
		for i := range tree {
			n := &tree[i]
			if math.IsNaN(n.Threshold) || math.IsInf(n.Threshold, 0) {
				return nil, fmt.Errorf("invalid river range threshold")
			}
			if n.Feature == -1 {
				if len(n.Values) == 0 {
					return nil, fmt.Errorf("empty river range leaf")
				}
				for j, v := range n.Values {
					if v.Count <= 0 || v.Count > 1_000_000_000 || (j > 0 && n.Values[j-1].Value >= v.Value) {
						return nil, fmt.Errorf("invalid river value distribution")
					}
				}
				n.rangeValues = makeRange(n.Values)
			} else if n.Feature < 0 || n.Feature >= cfr.PolicyFeatureCount || (n.Feature >= 11 && n.Feature < 28) || n.Left <= i || n.Right <= i || n.Left >= len(tree) || n.Right >= len(tree) {
				return nil, fmt.Errorf("invalid public river range branch")
			}
		}
	}
	return m, nil
}

func (m *riverRangeModel) equity(v *cfr.View) float64 {
	x := cfr.PolicyFeatures(v)
	value := deuce.Eval(v.Hand[:])
	e := 0.0
	for _, tree := range m.Forest {
		i := 0
		for tree[i].Feature >= 0 {
			n := &tree[i]
			if x[n.Feature] <= n.Threshold {
				i = n.Left
			} else {
				i = n.Right
			}
		}
		e += tree[i].rangeValues.equity(value)
	}
	return e / float64(len(m.Forest))
}

func (m *riverRangeModel) call(v *cfr.View) (int, bool) {
	if m == nil || v.Street != cfr.Draw3 || !v.Facing || v.ToCall <= 0 || v.Pot < v.ToCall {
		return cfr.Pass, false
	}
	price := float64(v.ToCall) / float64(v.Pot+v.ToCall)
	if m.equity(v) > price+m.Margin {
		return cfr.Pass, true
	}
	return cfr.Fold, true
}
