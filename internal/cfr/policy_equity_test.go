package cfr

import (
	"encoding/json"
	"math"
	"testing"
)

func TestPolicyEquityFeaturesDescribeDrawingPotential(t *testing.T) {
	v := View{Hand: five("9c", "Td", "Jh", "Qs", "Kc"), Node: 0}
	x := PolicyFeatures(&v)
	if len(x) != 64 {
		t.Fatalf("got %d features, want 64", len(x))
	}
	values := x[:]
	for i := 60; i < 64; i++ {
		if math.IsNaN(values[i]) || values[i] < 0 || values[i] > 1 {
			t.Fatalf("invalid equity feature %d: %v", i, values[i])
		}
		if i > 60 && values[i] < values[i-1]-1e-12 {
			t.Fatal("allowing another draw reduced best-keep equity")
		}
	}
	if values[61] <= values[60] {
		t.Fatal("breaking a high straight has no drawing potential")
	}
	v.Hand[0], v.Hand[4] = v.Hand[4], v.Hand[0]
	v.Node, v.Seat, v.Street = 100, BB, Draw3
	y := PolicyFeatures(&v)
	for i := 60; i < 64; i++ {
		if values[i] != y[i] {
			t.Fatal("private equity depends on card order or public context")
		}
	}
}

func TestEmpiricalEquityVersionPredictionAndMemo(t *testing.T) {
	m := Empirical{Version: 7}
	for i := range m.BettingForest {
		m.BettingForest[i] = [][]PolicyNode{{
			{Feature: 60, Threshold: .5, Left: 1, Right: 2},
			{Feature: -1, Prob: [6]float64{0, 1}},
			{Feature: -1, Prob: [6]float64{0, 0, 1}},
		}}
	}
	for i := range m.DrawingForest {
		m.DrawingForest[i] = [][]PolicyNode{{
			{Feature: 60, Threshold: .5, Left: 1, Right: 2},
			{Feature: -1, Prob: [6]float64{0, 1}},
			{Feature: -1, Prob: [6]float64{1}},
		}}
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	model, err := DecodeEmpirical(raw)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := model.WithMemoBits(4)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []*Empirical{model, cached, cached} {
		v := View{Hand: five("2c", "3d", "4h", "5s", "7c"), Street: Draw1, CanRaise: true}
		if a, ok := candidate.Bet(&v); !ok || a != Aggr {
			t.Fatal("strong hand ignored equity feature")
		}
		if keep, ok := candidate.Draw(&v); !ok || keep != 31 {
			t.Fatal("strong hand draw ignored equity feature")
		}
		v.Hand = five("9c", "Td", "Jh", "Qs", "Kc")
		if a, ok := candidate.Bet(&v); !ok || a != Pass {
			t.Fatal("weak hand ignored equity feature")
		}
		if keep, ok := candidate.Draw(&v); !ok || keep == 31 {
			t.Fatal("weak hand draw ignored equity feature")
		}
	}
	m.Version = 6
	raw, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeEmpirical(raw); err == nil {
		t.Fatal("version 6 accepted a version 7 feature")
	}
}
