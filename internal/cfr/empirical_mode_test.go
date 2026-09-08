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

func TestModeBetDeclinesUncertainActions(t *testing.T) {
	m := &Empirical{Version: 1}
	m.Betting[0] = []PolicyNode{{Feature: -1, Prob: [6]float64{2, 3, 5}}}
	v := View{Hand: five("2c", "3d", "4h", "7s", "Kc"), Facing: true, CanRaise: true}
	if _, ok := m.ModeBet(&v, .75); ok {
		t.Fatal("accepted a 50 percent action")
	}
	if a, ok := m.ModeBet(&v, .5); !ok || a != Aggr {
		t.Fatalf("most likely action %d %v", a, ok)
	}
	v.CanRaise = false
	if a, ok := m.ModeBet(&v, .55); !ok || a != Pass {
		t.Fatalf("legal probabilities were not renormalized: %d %v", a, ok)
	}
	for _, p := range []float64{-1, 2} {
		if _, ok := m.ModeBet(&v, p); ok {
			t.Fatal("accepted invalid threshold")
		}
	}
	m.Betting[0][0].Prob = [6]float64{0, 0, 1}
	if _, ok := m.ModeBet(&v, .75); ok {
		t.Fatal("treated zero legal support as confident")
	}
	if a, ok := m.ModeBet(&v, 0); !ok || a != Pass {
		t.Fatal("changed legacy no-support fallback")
	}
}
