package onyx

import (
	"encoding/json"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/deuce"
)

func TestRiverRangePricesBluffsAndTies(t *testing.T) {
	bluff := deuce.Eval(cards.MustParse("2c", "3d", "4h", "5s", "6c"))
	value := deuce.Eval(cards.MustParse("2d", "3h", "4s", "5c", "7d"))
	m := &riverRangeModel{Margin: .02, Forest: [][]riverRangeNode{{{Feature: -1, Values: []weightedValue{{uint32(bluff), 20}, {uint32(value), 80}}}}}}
	raw, err := json.Marshal(map[string]any{"river_range": m})
	if err != nil {
		t.Fatal(err)
	}
	m, err = decodeRiverRange(raw)
	if err != nil {
		t.Fatal(err)
	}
	v := cfr.View{Street: cfr.Draw3, Facing: true, Pot: 900, ToCall: 100}
	copy(v.Hand[:], cards.MustParse("2c", "3d", "4h", "7s", "Ac"))
	if e := m.equity(&v); e != .2 {
		t.Fatalf("catcher equity %f", e)
	}
	if a, ok := m.call(&v); !ok || a != cfr.Pass {
		t.Fatalf("profitable call %d %v", a, ok)
	}
	v.Pot = 200
	if a, ok := m.call(&v); !ok || a != cfr.Fold {
		t.Fatalf("ignored call price %d %v", a, ok)
	}
	copy(v.Hand[:], cards.MustParse("2c", "3d", "4h", "5s", "6c"))
	if e := m.equity(&v); e != .1 {
		t.Fatalf("tie equity %f", e)
	}
	copy(v.Hand[:], cards.MustParse("2d", "3h", "4s", "5c", "7d"))
	if e := m.equity(&v); e != .6 {
		t.Fatalf("stronger hand equity %f", e)
	}
	v.Street = cfr.Draw2
	if _, ok := m.call(&v); ok {
		t.Fatal("answered before river")
	}
}

func TestRiverRangeRejectsUnsafeModels(t *testing.T) {
	for _, raw := range []string{
		`{}`, `{"river_range":{"margin":0,"forest":[]}}`,
		`{"river_range":{"margin":-1,"forest":[[{"feature":-1,"values":[{"value":1,"count":1}]}]]}}`,
		`{"river_range":{"forest":[[{"feature":11,"left":1,"right":1},{"feature":-1,"values":[{"value":1,"count":1}]}]]}}`,
		`{"river_range":{"forest":[[{"feature":0,"left":0,"right":0}]]}}`,
		`{"river_range":{"forest":[[{"feature":-1,"values":[{"value":2,"count":1},{"value":1,"count":1}]}]]}}`,
		`{"river_range":{"forest":[[{"feature":-1,"values":[{"value":1,"count":0}]}]]}}`,
	} {
		if _, err := decodeRiverRange([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestRiverRangeConditionsOnObservedDrawCount(t *testing.T) {
	value := uint32(deuce.Eval(cards.MustParse("2d", "3h", "4s", "5c", "7d")))
	bluff := uint32(deuce.Eval(cards.MustParse("2c", "3d", "4h", "5s", "6c")))
	m := &riverRangeModel{Forest: [][]riverRangeNode{{
		{Feature: 10, Threshold: .5, Left: 1, Right: 2},
		{Feature: -1, Values: []weightedValue{{value, 1}}},
		{Feature: -1, Values: []weightedValue{{bluff, 1}}},
	}}}
	raw, err := json.Marshal(map[string]any{"river_range": m})
	if err != nil {
		t.Fatal(err)
	}
	m, err = decodeRiverRange(raw)
	if err != nil {
		t.Fatal(err)
	}
	v := cfr.View{Seat: 0, Street: cfr.Draw3, Facing: true, Pot: 900, ToCall: 100}
	copy(v.Hand[:], cards.MustParse("2c", "3d", "4h", "7s", "Ac"))
	if e := m.equity(&v); e != 0 {
		t.Fatalf("pat branch %f", e)
	}
	v.Drawn[1][3] = 1
	if e := m.equity(&v); e != 1 {
		t.Fatalf("draw branch %f", e)
	}
}
