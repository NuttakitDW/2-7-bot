package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cfr"
)

func TestStatsBaseBlueprintUsesLoadedLayout(t *testing.T) {
	w := newWorld()
	base := w.layout.WithoutFixed()
	bp := &cfr.Blueprint{Bet: make([]byte, base.BetSlots), Draw: make([]byte, base.DrawSlots)}
	path := filepath.Join(t.TempDir(), "base.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := bp.Encode(f); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stats([]string{"-bp", "base:" + path}); err != nil {
		t.Fatal(err)
	}
}
