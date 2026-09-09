package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cfr"
)

func TestSelectBlueprintPreservesExistingOutput(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "existing.gz")
	want := []byte("another experiment")
	if err := os.WriteFile(dest, want, 0600); err != nil {
		t.Fatal(err)
	}
	if err := selectBlueprint([]string{"-bp", "missing.gz", "-out", dest}); err == nil {
		t.Fatal("accepted existing output")
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("existing output changed: %q %v", got, err)
	}
}

func TestSelectSet(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		in, want   []byte
	}{
		{"identity", "0", []byte{30, 100, 125}, []byte{30, 100, 125}},
		{"drop small action", "0.3", []byte{25, 102, 128}, []byte{0, 113, 142}},
		{"mode", "mode", []byte{25, 102, 128}, []byte{0, 0, 255}},
		{"tie uses first", "mode", []byte{100, 100, 55}, []byte{255, 0, 0}},
		{"untrained remains untrained", "mode", []byte{0, 0, 0}, []byte{0, 0, 0}},
		{"empty filtered mix falls back", "0.9", []byte{85, 85, 85}, []byte{0, 0, 0}},
		{"draw padding stays zero", "mode", []byte{10, 245, 0, 0, 0, 0}, []byte{0, 255, 0, 0, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modes, err := parseSelections(tc.mode, 1)
			if err != nil {
				t.Fatal(err)
			}
			selectSet(tc.in, modes[0])
			if !bytes.Equal(tc.in, tc.want) {
				t.Fatalf("got %v, want %v", tc.in, tc.want)
			}
		})
	}
}

func TestApplySelectionsUsesStreetAndLayoutOffsets(t *testing.T) {
	w := newWorld()
	bp := &cfr.Blueprint{Bet: make([]byte, w.layout.BetSlots), Draw: make([]byte, w.layout.DrawSlots)}
	type probe struct {
		data []byte
		mode bool
	}
	var probes []probe
	bets, _ := parseSelections("0,mode,0,mode", cfr.Streets)
	draws, _ := parseSelections("mode,0,mode", cfr.Streets-1)
	for group := 0; group < w.layout.FixedGroups; group++ {
		for street := 0; street < cfr.Streets; street++ {
			for i := range w.tree.Nodes {
				node := &w.tree.Nodes[i]
				if node.Kind != cfr.KindBet || int(node.Street) != street || (group > 0 && !w.layout.Sliced(node, group)) {
					continue
				}
				slot := w.layout.BetSlotFixed(node, cfr.BetContexts(street)-1, w.layout.Buckets(street)-1, group)
				data := bp.Bet[slot : slot+int64(len(node.Acts))]
				data[0], data[1] = 100, 155
				probes = append(probes, probe{data, bets[street].greedy})
				break
			}
		}
		for street := cfr.Draw1; street <= cfr.Draw3; street++ {
			for seat := 0; seat < 2; seat++ {
				slot := w.layout.DrawSlotFixed(street, seat, 2, 15, w.abs.NumDrawClasses-1, group)
				data := bp.Draw[slot : slot+cfr.MaxCand]
				data[0], data[1] = 100, 155
				probes = append(probes, probe{data, draws[street-cfr.Draw1].greedy})
			}
		}
	}
	applySelections(w, bp, bets, draws)
	for i, p := range probes {
		want := []byte{100, 155}
		if p.mode {
			want = []byte{0, 255}
		}
		if !bytes.Equal(p.data[:2], want) {
			t.Errorf("probe %d: got %v want %v", i, p.data, want)
		}
		for _, b := range p.data[2:] {
			if b != 0 {
				t.Errorf("probe %d: padding changed", i)
			}
		}
	}
}

func TestParseSelections(t *testing.T) {
	for _, text := range []string{"", "NaN", "Inf", "-0.1", "1.1", "greedy", "0.3,mode"} {
		if _, err := parseSelections(text, 4); err == nil {
			t.Errorf("accepted %q", text)
		}
	}
	if got, err := parseSelections("0.3,mode,0.2,0", 4); err != nil || len(got) != 4 {
		t.Fatalf("per-street selections: %v %v", got, err)
	}
	if got, err := parseSelections("mode", 3); err != nil || len(got) != 3 || !got[2].greedy {
		t.Fatalf("broadcast: %v %v", got, err)
	}
}

func TestRestoreBasePolicy(t *testing.T) {
	candidate := &cfr.Blueprint{Bet: []byte{1, 2, 3, 4}, Draw: []byte{5, 6, 7}}
	base := &cfr.Blueprint{Bet: []byte{128, 127}, Draw: []byte{255}}
	if err := restoreBasePolicy(candidate, base); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(candidate.Bet, []byte{128, 127, 3, 4}) || !bytes.Equal(candidate.Draw, []byte{255, 6, 7}) {
		t.Fatal("base policy not restored exactly or appended group changed")
	}
	before := append([]byte(nil), candidate.Bet...)
	if err := restoreBasePolicy(candidate, &cfr.Blueprint{Bet: make([]byte, 5)}); err == nil {
		t.Fatal("accepted oversized base")
	}
	if !bytes.Equal(before, candidate.Bet) {
		t.Fatal("invalid base partially changed policy")
	}
}

func TestSelectRejectsInvalidCompression(t *testing.T) {
	for _, value := range []string{"0", "10", "-1"} {
		if err := selectBlueprint([]string{"-compression", value}); err == nil {
			t.Fatalf("accepted compression%s", value)
		}
	}
}
