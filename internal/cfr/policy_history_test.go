package cfr

import (
	"encoding/json"
	"testing"
)

func TestPolicyHistoryFeaturesUseOnlyPriorActions(t *testing.T) {
	tree := BuildTree()
	id := tree.Root
	check := func(seat int, want [24]float64) {
		t.Helper()
		v := View{Node: id, Seat: seat, Hand: five("2c", "3d", "4h", "7s", "Kc")}
		x := PolicyFeatures(&v)
		if len(x) != 64 {
			t.Fatalf("features=%d, want 64", len(x))
		}
		for i, value := range want {
			if x[32+i] != value {
				t.Fatalf("node=%d seat=%d history[%d]=%v want %v", id, seat, i, x[32+i], value)
			}
		}
	}
	step := func(action uint8) {
		t.Helper()
		for i, a := range tree.Nodes[id].Acts {
			if a == action {
				id = tree.Nodes[id].Next[i]
				return
			}
		}
		t.Fatalf("missing action %d", action)
	}
	// Each street: own raises, opponent raises, own checks, opponent
	// checks, own calls, opponent calls. Posting blinds is not an action.
	check(Btn, [24]float64{})
	step(Pass) // Button limps.
	check(BB, [24]float64{5: 1})
	step(Aggr) // Big blind raises.
	check(Btn, [24]float64{1: 1, 4: 1})
	step(Pass)
	check(BB, [24]float64{0: 1, 5: 2})
	id = tree.Nodes[id].Next[0] // BB draws.
	id = tree.Nodes[id].Next[0] // Button draws.
	check(BB, [24]float64{0: 1, 5: 2})
	step(Pass) // BB checks on draw1.
	check(Btn, [24]float64{1: 1, 4: 2, 9: 1})
	step(Aggr)
	check(BB, [24]float64{0: 1, 5: 2, 7: 1, 8: 1})
	// An unrelated root has no history even after traversing this branch.
	id = tree.Root
	check(Btn, [24]float64{})
}

func TestPolicyHistoryRejectsUnknownNode(t *testing.T) {
	for _, v := range []View{{Node: -1}, {Node: 1 << 30}} {
		v.Hand = five("2c", "3d", "4h", "7s", "Kc")
		x := PolicyFeatures(&v)
		for i := 32; i < 60; i++ {
			if x[i] != 0 {
				t.Fatal("invalid public node produced a history")
			}
		}
	}
}

func TestEmpiricalHistoryVersionAndPrediction(t *testing.T) {
	m := Empirical{Version: 5}
	for i := range m.BettingForest {
		m.BettingForest[i] = [][]PolicyNode{{
			{Feature: 35, Threshold: .5, Left: 1, Right: 2},
			{Feature: -1, Prob: [6]float64{0, 1}},
			{Feature: -1, Prob: [6]float64{0, 0, 1}},
		}}
	}
	for i := range m.DrawingForest {
		m.DrawingForest[i] = [][]PolicyNode{{{Feature: -1, Prob: [6]float64{1}}}}
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	model, err := DecodeEmpirical(raw)
	if err != nil {
		t.Fatal(err)
	}
	v := View{Hand: five("2c", "3d", "4h", "7s", "Kc"), CanRaise: true}
	if a, ok := model.Bet(&v); !ok || a != Pass {
		t.Fatal("root incorrectly contains future checking evidence")
	}
	tree := BuildTree()
	id := tree.Nodes[tree.Root].Next[1] // Button calls.
	id = tree.Nodes[id].Next[0]         // BB checks; enters draw1.
	v.Node, v.Street = id, Draw1
	if a, ok := model.Bet(&v); !ok || a != Aggr {
		t.Fatal("forest ignored prior opponent check")
	}
	m.Version = 4
	raw, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeEmpirical(raw); err == nil {
		t.Fatal("legacy model accepted a version 5 feature")
	}
}
