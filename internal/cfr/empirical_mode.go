package cfr

// Mode treats uncertainty in the fitted probabilities as uncertainty about
// a fixed policy, selecting its most likely legal action. It leaves the
// original stochastic model unchanged. Neither interpretation is exact;
// evaluate their transfer to the real opponent separately.
func (m *Empirical) Mode() Model { return empiricalMode{model: m} }

type empiricalMode struct{ model *Empirical }

// WithMemo gives a walker a privately cached copy of the underlying model.
func (m empiricalMode) WithMemo() Model {
	return empiricalMode{model: m.model.WithMemo().(*Empirical)}
}

func (m empiricalMode) Bet(v *View) (int, bool) {
	return m.model.ModeBet(v, 0)
}

// ModeBet can defer to another strategy when the largest fitted legal
// action probability is small. The threshold is not a calibrated certainty.
func (m *Empirical) ModeBet(v *View, minimum float64) (int, bool) {
	if m == nil || !(minimum >= 0 && minimum <= 1) {
		return Pass, false
	}
	p := m.predictBet(v)
	if !v.Facing {
		p[Fold] = 0
	}
	if !v.CanRaise {
		p[Aggr] = 0
	}
	total := p[Fold] + p[Pass] + p[Aggr]
	if total == 0 {
		return Pass, minimum == 0
	}
	a, ok := mostLikelyPolicy(p, 3)
	return a, ok && p[a]/total >= minimum
}

func (m empiricalMode) Draw(v *View) (uint8, bool) {
	if m.model == nil {
		return 0, false
	}
	n, ok := mostLikelyPolicy(m.model.drawProbabilities(v), 6)
	if !ok {
		return 0, false
	}
	return m.model.keepForCount(v, n), true
}

func mostLikelyPolicy(p [6]float64, n int) (int, bool) {
	best := 0
	for i := 1; i < n; i++ {
		if p[i] > p[best] {
			best = i
		}
	}
	return best, p[best] > 0
}
