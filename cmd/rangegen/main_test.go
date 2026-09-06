package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/arena"
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
)

func ptr[T any](v T) *T { return &v }
func sample() arena.HandDetail {
	return arena.HandDetail{Events: []arena.HandEvent{
		{Kind: "initial-cards", Player: ptr(1), Cards: ptr("2c3d4h7sAc")},
		{Kind: "replacement", Player: ptr(1), Street: ptr("draw2"), Cards: ptr("Kc"), Detail: ptr("Ac")},
		{Kind: "replacement", Player: ptr(1), Street: ptr("draw3"), Cards: ptr("5c"), Detail: ptr("Kc")},
		{Kind: "replacement", Player: ptr(0), Street: ptr("draw3"), Cards: ptr(""), Detail: ptr("")},
		{Kind: "action", Player: ptr(1), Street: ptr("draw3"), Action: ptr("bet")},
		{Kind: "action", Player: ptr(1), Street: ptr("draw3"), Action: ptr("raise")},
	}}
}

func TestReadFinalHandAndConditionalBets(t *testing.T) {
	h := sample()
	v, current, previous, ok, err := finalHand(h, 1)
	want := deuce.Eval(cards.MustParse("2c", "3d", "4h", "7s", "5c"))
	if err != nil || !ok || v != want || current != 1 || previous != 1 {
		t.Fatalf("%v %d %d %t %v", v, current, previous, ok, err)
	}
	bets, err := riverBets(h, 1)
	if err != nil || len(bets) != 2 || bets[0].context != 10 || bets[1].context != 14 || bets[0].value != want {
		t.Fatalf("%+v %v", bets, err)
	}
	if _, _, _, ok, err := finalHand(h, 0); ok || err != nil {
		t.Fatalf("incomplete opponent should not produce sample: %t %v", ok, err)
	}
}

func TestRejectMalformedCardsAndEvents(t *testing.T) {
	for _, text := range []string{"2", "Xc", "2x"} {
		if _, err := packed(text); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
	for i := 0; i < 3; i++ {
		h := sample()
		switch i {
		case 0:
			h.Events[0].Cards = nil
		case 1:
			h.Events[1].Detail = nil
		case 2:
			h.Events[2].Cards = ptr("2c")
		}
		if _, _, _, _, err := finalHand(h, 1); err == nil {
			t.Fatalf("accepted malformed final sample %d", i)
		}
		if _, err := riverBets(h, 1); err == nil {
			t.Fatalf("accepted malformed bet sample %d", i)
		}
	}
}

func TestGeneratorProducesDeterministicSource(t *testing.T) {
	dir := t.TempDir()
	metadata := filepath.Join(dir, "matches.json")
	hands := filepath.Join(dir, "hands")
	if err := os.Mkdir(hands, 0755); err != nil {
		t.Fatal(err)
	}
	ms, _ := json.Marshal([]arena.MatchSummary{{ID: 1, Players: []string{"hero", "target"}}})
	if err := os.WriteFile(metadata, ms, 0644); err != nil {
		t.Fatal(err)
	}
	h, _ := json.Marshal(sample())
	if err := os.WriteFile(filepath.Join(hands, "1-1.json"), h, 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "table.go")
	if err := generate(metadata, hands, "target", out); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "1 sampled final hands") {
		t.Fatalf("unexpected output: %s", first)
	}
	if err := generate(metadata, hands, "target", out); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(out)
	if string(first) != string(second) {
		t.Fatal("generation is not deterministic")
	}
	if err := generate(metadata, hands, "absent", out); err == nil {
		t.Fatal("accepted missing target")
	}
	if err := generate("missing", hands, "target", out); err == nil {
		t.Fatal("accepted missing metadata")
	}
}
