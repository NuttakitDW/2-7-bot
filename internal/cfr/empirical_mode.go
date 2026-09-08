package cfr

import "math"

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

// Sharpened raises the fitted probabilities to a power before sampling:
// alpha 1 is the fitted model, larger values approach Mode while keeping
// some mixing where the fit is unsure. Like Mode it is an interpretation
// of the fit, not a calibrated one.
func (m *Empirical) Sharpened(alpha float64) Model { return empiricalSharp{model: m, alpha: alpha} }

type empiricalSharp struct {
	model *Empirical
	alpha float64
}

func (m empiricalSharp) WithMemo() Model {
	return empiricalSharp{model: m.model.WithMemo().(*Empirical), alpha: m.alpha}
}

func sharpen(p [6]float64, n int, alpha float64) [6]float64 {
	var out [6]float64
	for i := 0; i < n; i++ {
		if p[i] > 0 {
			out[i] = math.Pow(p[i], alpha)
		}
	}
	return out
}

func (m empiricalSharp) Bet(v *View) (int, bool) {
	p := m.model.predictBet(v)
	if !v.Facing {
		p[Fold] = 0
	}
	if !v.CanRaise {
		p[Aggr] = 0
	}
	return samplePolicy(sharpen(p, 3, m.alpha), 3, v.Rand)
}

func (m empiricalSharp) Draw(v *View) (uint8, bool) {
	n, ok := samplePolicy(sharpen(m.model.predictDraw(v), 6, m.alpha), 6, v.Rand)
	if !ok {
		return 0, false
	}
	return m.model.keepForCount(v, n), true
}
