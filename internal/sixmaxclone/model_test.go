package sixmaxclone

import (
	"reflect"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

func TestBuildFeaturesPreservesH5BBettingOrderAndScale(t *testing.T) {
	snapshot := Snapshot{Hand: cards.MustParse("2c", "3d", "7h", "7s", "Kc"), Position: 5,
		ActivePlayers: 4, Aggressions: 2, Calls: 1, OwnRaises: 1, Pot: 750, Call: 250, SmallBet: 100}
	features, err := BuildFeatures(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !features.Facing || features.HasDeuce != 1 || features.KeepCount != 3 || features.PairCount != 1 ||
		features.Position != 2 || features.PotSmallBets != 7 || features.CallSmallBets != 2 {
		t.Fatalf("features=%+v", features)
	}
	scaled := snapshot
	scaled.Pot, scaled.Call, scaled.SmallBet = 7500, 2500, 1000
	got, err := BuildFeatures(scaled)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, features) {
		t.Fatalf("scaled=%+v want=%+v", got, features)
	}
}

func TestPredictHonorsConfidenceGate(t *testing.T) {
	model := Model{Schema: SchemaVersion, MinGroups: 20, MinConfidence: .65, Trees: map[string]Tree{
		"facing":  {Nodes: []Node{{Leaf: &Leaf{Groups: 30, Probabilities: [3]float64{.1, .15, .75}}}}},
		"checked": {Nodes: []Node{{Leaf: &Leaf{Groups: 30, Probabilities: [3]float64{.3, .35, .35}}}}},
	}}
	if err := model.Validate(); err != nil {
		t.Fatal(err)
	}
	if action, ok := model.Predict(Features{Facing: true}); !ok || action != Aggressive {
		t.Fatalf("prediction=%v/%t", action, ok)
	}
	if _, ok := model.Predict(Features{}); ok {
		t.Fatal("ambiguous leaf passed confidence gate")
	}
}
