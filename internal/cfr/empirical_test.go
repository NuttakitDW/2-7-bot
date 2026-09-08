package cfr

import (
	"encoding/json"
	"math/bits"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/policy"
)

func TestEmpiricalLegalActions(t *testing.T) {
	m := Empirical{Version: 1}
	for i := range m.Betting {
		m.Betting[i] = []PolicyNode{{Feature: -1, Prob: [6]float64{.7, .2, .1}}}
	}
	for i := range m.Drawing {
		m.Drawing[i] = []PolicyNode{{Feature: -1, Prob: [6]float64{0, 0, 0, 0, 0, 1}}}
	}
	raw, _ := json.Marshal(m)
	model, err := DecodeEmpirical(raw)
	if err != nil {
		t.Fatal(err)
	}
	v := View{Street: 0, Seat: 0, Rand: .9}
	for i, text := range []string{"2c", "3d", "4h", "7s", "9c"} {
		v.Hand[i], _ = cards.ParseCard(text)
	}
	if a, ok := model.Bet(&v); !ok || a != Pass {
		t.Fatalf("free capped action = %d,%v", a, ok)
	}
	v.Facing, v.CanRaise = true, true
	v.Rand = .1
	if a, _ := model.Bet(&v); a != Fold {
		t.Fatalf("facing action=%d", a)
	}
	v.Street = 1
	if keep, ok := model.Draw(&v); !ok || keep != 0 {
		t.Fatalf("draw five = %d,%v", keep, ok)
	}
}

func TestEmpiricalVersionThreePrefersDistinctRetainedRanks(t *testing.T) {
	v := View{Street: Draw3, Hand: five("2c", "2d", "4h", "8s", "9c")}
	for _, tt := range []struct {
		version int
		want    uint8
	}{{2, 15}, {3, 29}} {
		m := Empirical{Version: tt.version}
		for i := range m.Betting {
			m.Betting[i] = []PolicyNode{{Feature: -1, Prob: [6]float64{0, 1}}}
		}
		for i := range m.Drawing {
			m.Drawing[i] = []PolicyNode{{Feature: -1, Prob: [6]float64{0, 1}}}
		}
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeEmpirical(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := decoded.Draw(&v); !ok || got != tt.want {
			t.Fatalf("version %d keep=%d,%v want %d", tt.version, got, ok, tt.want)
		}
	}
	model := Empirical{Version: 3}
	for id := 0; id < handclass.Num; id++ {
		if handclass.Weight(handclass.ID(id)) == 0 {
			continue
		}
		copy(v.Hand[:], cards.SortedByRank(handclass.Representative(handclass.ID(id))))
		for count := 0; count <= 5; count++ {
			keep := model.keepForCount(&v, count)
			if bits.OnesCount8(keep) != 5-count || keep > 31 {
				t.Fatalf("invalid keep for class %d count %d: %d", id, count, keep)
			}
			if count == 0 {
				continue
			}
			ranks := map[cards.Rank]bool{}
			for i, c := range v.Hand {
				if keep&(1<<i) != 0 {
					ranks[c.Rank] = true
				}
			}
			if len(ranks) != min(5-count, len(cards.DistinctRanks(v.Hand[:]))) {
				t.Fatalf("retained unnecessary pair class %d count %d", id, count)
			}
		}
	}
}

func TestEmpiricalRejectsBadTrees(t *testing.T) {
	for _, raw := range []string{`{}`, `{"version":1}`, `{"version":2}`} {
		if _, err := DecodeEmpirical([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func BenchmarkPolicyFeatures(b *testing.B) {
	v := View{Street: 2}
	for i, text := range []string{"2c", "3d", "4h", "7s", "Kc"} {
		v.Hand[i], _ = cards.ParseCard(text)
	}
	for b.Loop() {
		_ = PolicyFeatures(&v)
	}
}

func TestGreedyBlueprintUsesMostLikelyTrainedAction(t *testing.T) {
	pl := Player{Greedy: true}
	if i, ok := pl.choose([]uint8{20, 200, 35}, .01); !ok || i != 1 {
		t.Fatalf("greedy action=%d,%v", i, ok)
	}
	if _, ok := pl.choose([]uint8{0, 0, 0}, .5); ok {
		t.Fatal("untrained set should fall back")
	}
}

func TestPolicyFeaturesIncludePrice(t *testing.T) {
	v := View{Hand: five("2c", "3d", "4h", "7s", "Kc"), Pot: 600, ToCall: 200}
	x := PolicyFeatures(&v)
	if x[29] != 3 || x[30] != 1 || x[31] != .25 {
		t.Fatalf("pot features=%v", x[29:])
	}
}

func TestPolicyFeaturesPreserveAllPrivateHandClasses(t *testing.T) {
	for id := 0; id < handclass.Num; id++ {
		if handclass.Weight(handclass.ID(id)) == 0 {
			continue
		}
		hand := cards.SortedByRank(handclass.Representative(handclass.ID(id)))
		v := View{Hand: [5]cards.Card(hand), Seat: id % 2, Street: id % 4, Pot: 800, ToCall: 200}
		x := PolicyFeatures(&v)
		var want [17]float64
		val := deuce.Eval(hand)
		want[0], want[1] = float64(val), float64(val.Class())
		for i, c := range hand {
			want[2+i] = float64(c.Rank)
			if c.Rank <= cards.Seven {
				want[13]++
			}
			if c.Rank <= cards.Nine {
				want[14]++
			}
		}
		keep := policy.DrawingKeep(hand)
		want[7] = float64(len(keep))
		for i, r := range keep {
			want[8+i] = float64(r)
		}
		want[15] = float64(len(cards.DistinctRanks(hand)))
		if cards.SameSuit(hand) {
			want[16] = 1
		}
		if [17]float64(x[11:28]) != want || x[0] != float64(v.Seat) || x[28] != float64(v.Street) || x[31] != .2 {
			t.Fatalf("features changed for class %d", id)
		}
	}
}

func TestEmpiricalForestAveragesNormalizedTrees(t *testing.T) {
	m := Empirical{Version: 4}
	for i := range m.BettingForest {
		m.BettingForest[i] = [][]PolicyNode{{{Feature: -1, Prob: [6]float64{8, 2}}}, {{Feature: -1, Prob: [6]float64{0, 0, 1}}}}
	}
	for i := range m.DrawingForest {
		m.DrawingForest[i] = [][]PolicyNode{{{Feature: -1, Prob: [6]float64{2}}}, {{Feature: -1, Prob: [6]float64{0, 0, 0, 0, 0, 7}}}}
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	model, err := DecodeEmpirical(raw)
	if err != nil {
		t.Fatal(err)
	}
	v := View{Hand: five("2c", "3d", "4h", "7s", "Kc"), Facing: true, CanRaise: true, Rand: .45}
	if a, ok := model.Bet(&v); !ok || a != Pass {
		t.Fatalf("mixed action %d,%v", a, ok)
	}
	if got := model.betProbabilities(&v); got != [6]float64{.4, .1, .5} {
		t.Fatalf("posterior probabilities %v", got)
	}
	v.CanRaise = false
	v.Rand = .81
	if a, ok := model.Bet(&v); !ok || a != Pass {
		t.Fatalf("capped action %d,%v", a, ok)
	}
	v.Street = 1
	v.Rand = .75
	if keep, ok := model.Draw(&v); !ok || keep != 0 {
		t.Fatalf("mixed draw %d,%v", keep, ok)
	}
	for _, mutate := range []func(*Empirical){
		func(m *Empirical) { m.BettingForest[0] = nil },
		func(m *Empirical) { m.DrawingForest[0][0] = nil },
		func(m *Empirical) { m.Betting[0] = []PolicyNode{{Feature: -1, Prob: [6]float64{1}}} },
		func(m *Empirical) { m.Version = 3 },
	} {
		var broken Empirical
		if err := json.Unmarshal(raw, &broken); err != nil {
			t.Fatal(err)
		}
		mutate(&broken)
		bad, err := json.Marshal(broken)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeEmpirical(bad); err == nil {
			t.Fatal("accepted invalid forest model")
		}
	}
}
