package cfr

import "testing"

func TestEmpiricalModeSelectsMostLikelyLegalAction(t *testing.T) {
	m := &Empirical{Version: 4}
	for i := range m.BettingForest {
		m.BettingForest[i] = [][]PolicyNode{{{Feature: -1, Prob: [6]float64{2, 3, 5}}}}
	}
	for i := range m.DrawingForest {
		m.DrawingForest[i] = [][]PolicyNode{{{Feature: -1, Prob: [6]float64{1, 8, 1}}}}
	}
	mode := m.Mode()
	v := View{Hand: five("2c", "3d", "4h", "7s", "Kc"), Facing: true, CanRaise: true}
	for _, r := range []float64{0, .3, .9} {
		v.Rand = r
		if a, ok := mode.Bet(&v); !ok || a != Aggr {
			t.Fatalf("mode bet %d,%v", a, ok)
		}
	}
	v.CanRaise = false
	if a, ok := mode.Bet(&v); !ok || a != Pass {
		t.Fatal("mode selected an unavailable raise")
	}
	v.Street = Draw1
	if keep, ok := mode.Draw(&v); !ok || keep != 15 {
		t.Fatalf("mode draw %d,%v", keep, ok)
	}
	// Wrapping the model does not change the original stochastic policy.
	v.Street, v.CanRaise, v.Rand = Predraw, true, 0
	if a, ok := m.Bet(&v); !ok || a != Fold {
		t.Fatal("mode wrapper mutated original policy")
	}
}
