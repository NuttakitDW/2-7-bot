package cfr

import (
	"encoding/json"
	"math"
	"os"
	"sync"
	"testing"
)

func TestBoostConcurrentFirstPrediction(t *testing.T) {
	b := boostFixture().BettingBoost[0]
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var x [PolicyFeatureCount]float64
			x[60] = .6
			for range 100 {
				if p := b.predict(&x); math.Abs(p[2]-.9) > 1e-12 {
					t.Errorf("concurrent prediction %v", p)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func BenchmarkBoostArtifact(b *testing.B) {
	path := os.Getenv("BOOST_MODEL_TEST_PATH")
	if path == "" {
		b.Skip("no fitted model supplied")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	m, err := DecodeEmpirical(raw)
	if err != nil {
		b.Fatal(err)
	}
	raw, err = os.ReadFile(os.Getenv("BOOST_CASES_TEST_PATH"))
	if err != nil {
		b.Fatal(err)
	}
	var cases []struct {
		Draw   bool
		Street int
		X      [PolicyFeatureCount]float64
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		b.Fatal(err)
	}
	if len(cases) == 0 {
		b.Fatal("no benchmark cases")
	}
	for _, c := range cases {
		if c.Draw {
			m.DrawingBoost[c.Street-1].predict(&c.X)
		} else {
			m.BettingBoost[c.Street].predict(&c.X)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := &cases[i%len(cases)]
		if c.Draw {
			m.DrawingBoost[c.Street-1].predict(&c.X)
		} else {
			m.BettingBoost[c.Street].predict(&c.X)
		}
	}
}

// Fitted models stay outside source control. This optional integration check
// compares their complete exported trees with probabilities from the fitter.
func TestBoostExportParity(t *testing.T) {
	path := os.Getenv("BOOST_MODEL_TEST_PATH")
	if path == "" {
		t.Skip("no fitted model supplied")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := DecodeEmpirical(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(os.Getenv("BOOST_CASES_TEST_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Draw   bool
		Street int
		X      [PolicyFeatureCount]float64
		Want   [6]float64
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no parity cases")
	}
	for i, c := range cases {
		var b *PolicyBoost
		if c.Draw {
			b = m.DrawingBoost[c.Street-1]
		} else {
			b = m.BettingBoost[c.Street]
		}
		got := b.predict(&c.X)
		for k, want := range c.Want {
			if math.Abs(got[k]-want) > 1e-11 {
				t.Fatalf("case %d class %d: %.15g != %.15g", i, k, got[k], want)
			}
		}
	}
}

func boostFixture() Empirical {
	m := Empirical{Version: 8}
	makeBoost := func() *PolicyBoost {
		return &PolicyBoost{Classes: []int{0, 2}, Bias: [6]float64{2: math.Log(3)}, Trees: []BoostTree{{Class: 2, Nodes: []BoostNode{
			{Feature: 60, Threshold: .5, Left: 1, Right: 2},
			{Feature: -1, Value: -math.Log(3)},
			{Feature: -1, Value: math.Log(3)},
		}}}}
	}
	for i := range m.BettingBoost {
		m.BettingBoost[i] = makeBoost()
	}
	for i := range m.DrawingBoost {
		m.DrawingBoost[i] = makeBoost()
	}
	return m
}

func TestBoostProbabilities(t *testing.T) {
	m := boostFixture()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeEmpirical(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ equity, want float64 }{{.5, .5}, {.6, .9}} {
		var x [PolicyFeatureCount]float64
		x[60] = tc.equity
		p := decoded.BettingBoost[0].predict(&x)
		if math.Abs(p[2]-tc.want) > 1e-12 || math.Abs(p[0]+p[2]-1) > 1e-12 {
			t.Fatalf("got %v", p)
		}
		for _, k := range []int{1, 3, 4, 5} {
			if p[k] != 0 {
				t.Fatal("absent class received probability")
			}
		}
	}
	// A large common offset must not overflow the softmax.
	decoded.BettingBoost[0].Bias[0] += 10000
	decoded.BettingBoost[0].Bias[2] += 10000
	x := [PolicyFeatureCount]float64{}
	if p := decoded.BettingBoost[0].predict(&x); math.Abs(p[2]-.5) > 1e-12 {
		t.Fatal(p)
	}
}

func TestBoostDecodeRejectsMalformedModels(t *testing.T) {
	cases := map[string]func(*Empirical){
		"old version":           func(m *Empirical) { m.Version = 7 },
		"missing round":         func(m *Empirical) { m.DrawingBoost[2] = nil },
		"duplicate class":       func(m *Empirical) { m.BettingBoost[0].Classes = []int{0, 0} },
		"invalid betting class": func(m *Empirical) { m.BettingBoost[0].Classes = []int{0, 5} },
		"absent tree class":     func(m *Empirical) { m.BettingBoost[0].Trees[0].Class = 1 },
		"absent bias class":     func(m *Empirical) { m.BettingBoost[0].Bias[1] = 1 },
		"cycle":                 func(m *Empirical) { m.BettingBoost[0].Trees[0].Nodes[0].Left = 0 },
		"feature":               func(m *Empirical) { m.BettingBoost[0].Trees[0].Nodes[0].Feature = 64 },
		"empty tree":            func(m *Empirical) { m.BettingBoost[0].Trees[0].Nodes = nil },
		"empty forest":          func(m *Empirical) { m.BettingBoost[0].Trees = nil },
		"excessive value":       func(m *Empirical) { m.BettingBoost[0].Trees[0].Nodes[1].Value = 1e100 },
		"mixed format":          func(m *Empirical) { m.BettingForest[0] = [][]PolicyNode{{{Feature: -1, Prob: [6]float64{1}}}} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := boostFixture()
			mutate(&m)
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = DecodeEmpirical(raw); err == nil {
				t.Fatal("accepted malformed boost")
			}
		})
	}
}

func TestBoostMemoAndLegalActions(t *testing.T) {
	m := boostFixture()
	raw, _ := json.Marshal(m)
	model, err := DecodeEmpirical(raw)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := model.WithMemoBits(4)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []*Empirical{model, cached, cached} {
		v := View{Hand: five("2c", "3d", "4h", "5s", "7c"), Street: Draw1, Facing: true, CanRaise: true, Rand: .5}
		if a, ok := candidate.Bet(&v); !ok || a != 2 {
			t.Fatalf("bet %d %v", a, ok)
		}
		if keep, ok := candidate.Draw(&v); !ok || keep == 31 {
			t.Fatalf("draw %d %v", keep, ok)
		}
		v.CanRaise = false
		if a, ok := candidate.Bet(&v); !ok || a != 0 {
			t.Fatalf("illegal raise: %d %v", a, ok)
		}
	}
}
