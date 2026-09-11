package main

import (
	"path/filepath"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/sixmaxdata"
	"github.com/nuttakit/2-7-bot/internal/sixmaxrange"
)

func TestHoldoutIsNotCountedAsTraining(t *testing.T) {
	rows := []sixmaxrange.Row{{MatchID: 102, Weight: 1, Group: "a"}, {MatchID: 103, Weight: 1, Group: "b"}}
	train, held := splitRows(rows, 103)
	if len(train) != 1 || train[0].MatchID != 102 || len(held) != 1 || held[0].MatchID != 103 {
		t.Fatalf("split=%v/%v", train, held)
	}
}

func TestLoadRowsRejectsDerivedMatchIdentityMismatch(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "match-41")
	meta := matchFile{}
	meta.MatchInfo.ID = 41
	meta.MatchInfo.DealMode = "duplicate"
	meta.MatchInfo.Players = make([]string, 6)
	if err := writeJSON(filepath.Join(dir, "match.json"), meta); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, "derived.json"), []sixmaxdata.HandRecord{{MatchID: 85}}); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRows(root, []int{41}); err == nil {
		t.Fatal("accepted mismatched derived match identity")
	}
}

func TestParseIDsDeduplicatesAndSorts(t *testing.T) {
	got, err := parseIDs("103, 41,103")
	if err != nil || len(got) != 2 || got[0] != 41 || got[1] != 103 {
		t.Fatalf("ids=%v err=%v", got, err)
	}
	if _, err := parseIDs("bad"); err == nil {
		t.Fatal("accepted invalid id")
	}
}
