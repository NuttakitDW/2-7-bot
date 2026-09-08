package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
)

func TestModelResolvesEmpiricalModeWithoutChangingMixedDefault(t *testing.T) {
	m := cfr.Empirical{Version: 3}
	for i := range m.Betting {
		m.Betting[i] = []cfr.PolicyNode{{Feature: -1, Prob: [6]float64{2, 3, 5}}}
	}
	for i := range m.Drawing {
		m.Drawing[i] = []cfr.PolicyNode{{Feature: -1, Prob: [6]float64{1}}}
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "model.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	w := &world{}
	v := cfr.View{Facing: true, CanRaise: true}
	for i, rank := range []cards.Rank{cards.Two, cards.Three, cards.Four, cards.Seven, cards.King} {
		v.Hand[i] = cards.Card{Rank: rank, Suit: cards.Clubs}
	}
	for _, tt := range []struct {
		name string
		want int
	}{{path, cfr.Fold}, {"mode:" + path, cfr.Aggr}} {
		model, err := w.model(tt.name, 0)
		if err != nil {
			t.Fatal(err)
		}
		if a, ok := model.Bet(&v); !ok || a != tt.want {
			t.Fatalf("%s action %d,%v want %d", tt.name, a, ok, tt.want)
		}
	}
	for _, name := range []string{"mode:cobalt", "mode:", "mode:" + filepath.Join(t.TempDir(), "missing.json")} {
		if _, err := w.model(name, 0); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
}
