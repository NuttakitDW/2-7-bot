package lapis

import (
	"encoding/json"
	"math/bits"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
)

func TestEmbeddedEmpiricalModalPolicy(t *testing.T) {
	old := blueprintData
	t.Cleanup(func() { blueprintData = old })
	m := cfr.Empirical{Version: 8}
	bet := &cfr.PolicyBoost{Classes: []int{0, 1, 2}, Bias: [6]float64{0, 0, 10}, Trees: []cfr.BoostTree{{Class: 2, Nodes: []cfr.BoostNode{{Feature: -1}}}}}
	draw := &cfr.PolicyBoost{Classes: []int{0, 1, 2, 3, 4, 5}, Bias: [6]float64{0, 0, 10}, Trees: []cfr.BoostTree{{Class: 2, Nodes: []cfr.BoostNode{{Feature: -1}}}}}
	for i := range m.BettingBoost {
		m.BettingBoost[i] = bet
	}
	for i := range m.DrawingBoost {
		m.DrawingBoost[i] = draw
	}
	var err error
	blueprintData, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewEmpirical()
	if err != nil {
		t.Fatal(err)
	}
	v := cfr.View{CanRaise: true, Facing: true}
	copy(v.Hand[:], cards.MustParse("7c", "5d", "4h", "3s", "2c"))
	for _, r := range []float64{0, .5, .999999} {
		v.Rand = r
		if a, ok := b.player.Bet(&v); !ok || a != cfr.Aggr {
			t.Fatalf("modal bet: %d %v", a, ok)
		}
		v.Street = 1
		keep, ok := b.player.Draw(&v)
		if !ok || bits.OnesCount8(keep) != 3 {
			t.Fatalf("draw: %b %v", keep, ok)
		}
	}
}

func TestEmbeddedEmpiricalRejectsInvalidPayload(t *testing.T) {
	old := blueprintData
	t.Cleanup(func() { blueprintData = old })
	for _, data := range [][]byte{old, []byte("invalid"), []byte(`{"version":99}`)} {
		blueprintData = data
		if _, err := NewEmpirical(); err == nil {
			t.Fatal("accepted invalid empirical artifact")
		}
	}
}
