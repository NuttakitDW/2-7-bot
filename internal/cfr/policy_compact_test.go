package cfr

import (
	"math"
	"math/rand/v2"
	"testing"
)

func randomTree(rng *rand.Rand, depth int) []PolicyNode {
	var nodes []PolicyNode
	var grow func(d int) int
	grow = func(d int) int {
		id := len(nodes)
		nodes = append(nodes, PolicyNode{})
		if d == 0 || rng.Float64() < .2 {
			var p [6]float64
			for i := range p {
				p[i] = rng.Float64()
			}
			nodes[id] = PolicyNode{Feature: -1, Prob: p}
			return id
		}
		feature := rng.IntN(PolicyFeatureCount)
		threshold := float64(rng.IntN(14)) + .5
		left := grow(d - 1)
		right := grow(d - 1)
		nodes[id] = PolicyNode{Feature: feature, Threshold: threshold, Left: left, Right: right}
		return id
	}
	grow(depth)
	return nodes
}

func TestCompactForestMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 9))
	forest := make([][]PolicyNode, 12)
	for i := range forest {
		forest[i] = randomTree(rng, 8)
	}
	compact := compileForest(forest)
	for trial := 0; trial < 500; trial++ {
		var x [PolicyFeatureCount]float64
		for i := range x {
			x[i] = float64(rng.IntN(15))
		}
		want := predictForest(forest, &x)
		got := predictCompact(compact, &x)
		for i := range want {
			if math.Abs(want[i]-got[i]) > 1e-12 {
				t.Fatalf("trial %d: got %v want %v", trial, got, want)
			}
		}
	}
}

func TestMemoReturnsComputedPrediction(t *testing.T) {
	m := &Empirical{Version: 4}
	for i := range m.BettingForest {
		m.BettingForest[i] = [][]PolicyNode{{{Feature: -1, Prob: [6]float64{1, 2, 7}}}}
	}
	for i := range m.DrawingForest {
		m.DrawingForest[i] = [][]PolicyNode{{{Feature: -1, Prob: [6]float64{1, 8, 1}}}}
	}
	m = m.WithMemo().(*Empirical)
	v := View{Hand: five("2c", "3d", "4h", "7s", "Kc"), Node: 0, Facing: true, CanRaise: true}
	for i := 0; i < 3; i++ {
		p := m.predictBet(&v)
		if math.Abs(p[Aggr]-.7) > 1e-6 {
			t.Fatalf("pass %d: %v", i, p)
		}
	}
	v.Street = Draw1
	if p := m.predictDraw(&v); math.Abs(p[1]-.8) > 1e-6 {
		t.Fatalf("draw %v", p)
	}
}
