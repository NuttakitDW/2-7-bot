package cfr

// Mode treats uncertainty in the fitted probabilities as uncertainty about
// a fixed policy, selecting its most likely legal action. It leaves the
// original stochastic model unchanged. Neither interpretation is exact;
// evaluate their transfer to the real opponent separately.
func (m *Empirical) Mode() Model { return empiricalMode{model: m} }

type empiricalMode struct{ model *Empirical }

func (m empiricalMode) Bet(v *View) (int, bool) {
	if m.model == nil {
		return Pass, false
	}
	p := m.model.betProbabilities(v)
	return mostLikelyPolicy(p, 3)
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
