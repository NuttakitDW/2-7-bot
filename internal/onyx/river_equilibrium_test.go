package onyx

import (
	"encoding/json"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
)

func equilibriumFixture(t *testing.T, tree *cfr.Tree) *riverEquilibrium {
	t.Helper()
	games := []equilibriumGame{{OOPTag: 2, IPTag: 2, Pot: 8, Exploitability: .0001, Strategies: []equilibriumRow{
		{Seat: 0, Hand: "2d3h4s6c9d", History: "r", P: [3]float64{1.0 / 3, 2.0 / 3, 0}},
		{Seat: 0, Hand: "2d3h4s6c7d", History: "r", P: [3]float64{0, 0, 1}},
	}}, {OOPTag: 2, IPTag: 2, Pot: 24, Exploitability: .0001, Strategies: []equilibriumRow{
		{Seat: 0, Hand: "2d3h4s6c9d", History: "r", P: [3]float64{0, 1, 0}},
	}}}
	raw, err := json.Marshal(map[string]any{"river_equilibrium": games})
	if err != nil {
		t.Fatal(err)
	}
	model, err := decodeRiverEquilibrium(raw, tree)
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func TestEquilibriumUsesPotHistoryHandAndMixedCalls(t *testing.T) {
	tree := cfr.BuildTree()
	model := equilibriumFixture(t, tree)
	var node int32 = -1
	for id, n := range tree.Nodes {
		if n.Street == 3 && n.Kind == cfr.KindBet && n.Wagers == 1 && n.Actor == 0 && model.contexts[id].History == "r" && model.contexts[id].Pot == 400 {
			node = int32(id)
			break
		}
	}
	if node < 0 {
		t.Fatal("missing river response")
	}
	v := cfr.View{Node: node, Seat: 0, Street: 3, Facing: true, CanRaise: true}
	v.Drawn[0][3], v.Drawn[1][3] = 1, 1
	copy(v.Hand[:], cards.MustParse("2d", "3h", "4s", "6c", "9d"))
	if a, ok := model.action(&v, .2); !ok || a != cfr.Fold {
		t.Fatalf("mixed fold %d %v", a, ok)
	}
	if a, ok := model.action(&v, .5); !ok || a != cfr.Pass {
		t.Fatalf("mixed call %d %v", a, ok)
	}
	copy(v.Hand[:], cards.MustParse("2d", "3h", "4s", "6c", "7d"))
	if a, ok := model.action(&v, .5); !ok || a != cfr.Aggr {
		t.Fatalf("value raise %d %v", a, ok)
	}
	v.CanRaise = false
	if _, ok := model.action(&v, .5); ok {
		t.Fatal("invented legal probability mass")
	}
	v.CanRaise = true
	copy(v.Hand[:], cards.MustParse("2d", "3h", "4s", "6c", "9d"))
	for id, n := range tree.Nodes {
		if n.Kind == cfr.KindBet && n.Street == 3 && n.Actor == 0 && model.contexts[id].History == "r" && model.contexts[id].Pot == 1200 {
			v.Node = int32(id)
			break
		}
	}
	if a, ok := model.action(&v, .1); !ok || a != cfr.Pass {
		t.Fatalf("ignored larger pot %d %v", a, ok)
	}
	v.Street = 2
	if _, ok := model.action(&v, .5); ok {
		t.Fatal("answered before river")
	}
	v.Street = 3
	v.Drawn[0][3] = -1
	if _, ok := model.action(&v, .5); ok {
		t.Fatal("answered with unknown draw")
	}
}

func TestEquilibriumRejectsInvalidExports(t *testing.T) {
	tree := cfr.BuildTree()
	for _, raw := range []string{`{}`, `{"river_equilibrium":[]}`, `{"river_equilibrium":[{"oop_tag":0,"ip_tag":0,"pot":8,"exploitability_chips":1,"strategies":[]}]}`, `{"river_equilibrium":[{"oop_tag":0,"ip_tag":0,"pot":8,"strategies":[{"seat":0,"hand":"2c2c4h5s7c","history":"r","p":[0,1,0]}]}]}`} {
		if _, err := decodeRiverEquilibrium([]byte(raw), tree); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
