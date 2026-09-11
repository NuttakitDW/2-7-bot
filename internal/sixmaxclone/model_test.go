package sixmaxclone

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

func TestFeaturesCapturePredrawPublicState(t *testing.T) {
	f, err := BuildFeatures(Snapshot{Hand: cards.MustParse("2c", "3d", "7h", "7s", "Kc"), Position: 5,
		ActivePlayers: 4, Aggressions: 2, Calls: 1, OwnRaises: 1, Pot: 3750, Call: 750, SmallBet: 500})
	if err != nil {
		t.Fatal(err)
	}
	if !f.Facing || f.HasDeuce != 1 || f.KeepCount != 3 || f.PairCount != 1 || f.Position != 2 || f.PotSmallBets != 7 || f.CallSmallBets != 1 {
		t.Fatalf("features = %+v", f)
	}
}

func TestBuildFeaturesNormalizesPositionsToBettingOrder(t *testing.T) {
	want := [...]int{3, 4, 5, 0, 1, 2}
	for input, expected := range want {
		features, err := BuildFeatures(Snapshot{Hand: cards.MustParse("2c", "3d", "7h", "7s", "Kc"),
			Position: input, ActivePlayers: 6, SmallBet: 100})
		if err != nil {
			t.Fatal(err)
		}
		if features.Position != expected {
			t.Errorf("position %d normalized to %d, want %d", input, features.Position, expected)
		}
	}
}

func TestBuildFeaturesRejectsInvalidNumericInputs(t *testing.T) {
	valid := Snapshot{Hand: cards.MustParse("2c", "3d", "7h", "7s", "Kc"), Position: 5,
		ActivePlayers: 4, Pot: 500, Call: 100, SmallBet: 100}
	tests := []struct {
		name   string
		mutate func(*Snapshot)
	}{
		{"negative aggressions", func(s *Snapshot) { s.Aggressions = -1 }},
		{"negative calls", func(s *Snapshot) { s.Calls = -1 }},
		{"negative own raises", func(s *Snapshot) { s.OwnRaises = -1 }},
		{"pot ratio overflows int", func(s *Snapshot) { s.Pot, s.SmallBet = ^uint64(0), 1 }},
		{"call ratio overflows int", func(s *Snapshot) { s.Call, s.SmallBet = ^uint64(0), 1 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := valid
			tc.mutate(&snapshot)
			if _, err := BuildFeatures(snapshot); err == nil {
				t.Fatal("invalid numeric input accepted")
			}
		})
	}
}

func TestBuildFeaturesBetUnitScaleInvariance(t *testing.T) {
	base := Snapshot{Hand: cards.MustParse("2c", "3d", "7h", "7s", "Kc"), Position: 5,
		ActivePlayers: 4, Aggressions: 2, Calls: 1, OwnRaises: 1, Pot: 750, Call: 250, SmallBet: 100}
	scaled := base
	scaled.Pot, scaled.Call, scaled.SmallBet = base.Pot*10, base.Call*10, base.SmallBet*10
	want, err := BuildFeatures(base)
	if err != nil {
		t.Fatal(err)
	}
	got, err := BuildFeatures(scaled)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scaled features = %+v, want %+v", got, want)
	}
}

func TestModelPredictAndConfidenceGate(t *testing.T) {
	m := Model{Schema: SchemaVersion, MinGroups: 20, MinConfidence: .55, MinMargin: .1,
		Trees: map[string]Tree{"facing": {Nodes: []Node{
			{Feature: FeatureAggressions, Threshold: .5, Left: 1, Right: 2},
			{Leaf: &Leaf{Groups: 30, Probabilities: [3]float64{.7, .2, .1}}},
			{Leaf: &Leaf{Groups: 40, Probabilities: [3]float64{.1, .25, .65}}},
		}}}}
	a, ok := m.Predict(Features{Facing: true, Aggressions: 2})
	if !ok || a != Aggressive {
		t.Fatalf("prediction = %q, %t", a, ok)
	}
	m.Trees["facing"] = Tree{Nodes: []Node{{Leaf: &Leaf{Groups: 40, Probabilities: [3]float64{.3, .35, .35}}}}}
	if _, ok := m.Predict(Features{Facing: true}); ok {
		t.Fatal("ambiguous leaf passed confidence gate")
	}
}

func TestDecodeRejectsMalformedTree(t *testing.T) {
	b, _ := json.Marshal(Model{Schema: SchemaVersion, MinGroups: 20, MinConfidence: .5, MinMargin: .1,
		Trees: map[string]Tree{
			"facing":  {Nodes: []Node{{Left: 9, Right: 9}}},
			"checked": {Nodes: []Node{{Leaf: &Leaf{Groups: 20, Probabilities: [3]float64{0, 1, 0}}}}},
		}})
	if _, err := Decode(b); err == nil {
		t.Fatal("malformed child indexes accepted")
	}
}

func TestModelValidateEnforcesReachableMaximumDepth(t *testing.T) {
	leaf := func() Node {
		return Node{Leaf: &Leaf{Groups: 20, Probabilities: [3]float64{0, 1, 0}}}
	}
	completeTree := func(depth int) Tree {
		var nodes []Node
		var add func(int) int
		add = func(remaining int) int {
			index := len(nodes)
			nodes = append(nodes, Node{})
			if remaining == 0 {
				nodes[index] = leaf()
				return index
			}
			left, right := add(remaining-1), add(remaining-1)
			nodes[index] = Node{Feature: FeatureCalls, Threshold: .5, Left: left, Right: right}
			return index
		}
		add(depth)
		return Tree{Nodes: nodes}
	}

	for _, tc := range []struct {
		name    string
		depth   int
		wantErr bool
	}{
		{"depth four", 4, false},
		{"depth five", 5, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := Model{Schema: SchemaVersion, MinGroups: 20, MinConfidence: .5, MinMargin: .1,
				Trees: map[string]Tree{"facing": completeTree(tc.depth), "checked": {Nodes: []Node{leaf()}}}}
			err := model.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %t", err, tc.wantErr)
			}
		})
	}
}

func TestNontrivialTreeRoundTripPreservesPrediction(t *testing.T) {
	leaf := func(p [3]float64) Node { return Node{Leaf: &Leaf{Groups: 25, Probabilities: p}} }
	m := Model{Schema: SchemaVersion, MinGroups: 20, MinConfidence: .5, MinMargin: .1, Trees: map[string]Tree{
		"facing":  {Nodes: []Node{{Feature: FeatureCalls, Threshold: .5, Left: 1, Right: 2}, leaf([3]float64{.7, .2, .1}), leaf([3]float64{.1, .2, .7})}},
		"checked": {Nodes: []Node{leaf([3]float64{0, .8, .2})}},
	}}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := decoded.Predict(Features{Facing: true, Calls: 2}); !ok || got != Aggressive {
		t.Fatalf("round-trip prediction = %v, %t", got, ok)
	}
}

func TestEmbeddedDefaultModelAlwaysDefers(t *testing.T) {
	model, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, facing := range []bool{false, true} {
		if _, ok := model.Predict(Features{Facing: facing}); ok {
			t.Fatalf("default embedded model predicted with facing=%t", facing)
		}
	}
}
