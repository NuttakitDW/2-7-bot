package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nuttakit/2-7-bot/internal/cfr"
)

type selection struct {
	floor  byte
	greedy bool
}

// selectBlueprint freezes street-specific action selection into a blueprint.
// Play the output with purify=0 and greedy=false. Empty filtered sets retain
// the existing runtime fallback behavior; untrained sets remain empty.
func selectBlueprint(args []string) error {
	fs := flag.NewFlagSet("select", flag.ContinueOnError)
	in := fs.String("bp", "", "source blueprint")
	out := fs.String("out", "", "new blueprint output (must differ from source)")
	compression := fs.Int("compression", gzip.BestCompression, "gzip level 1-9; 1 speeds up temporary experiment exports")
	basePath := fs.String("base", "", "with a fixed-card layout: restore this already-selected single-group policy after filtering")
	fallbackPath := fs.String("fallback", "", "fill empty filtered sets from this already-selected policy with the same layout, before base restoration")
	bets := fs.String("bet", "0.3", "probability floor or mode; one value or four comma-separated streets")
	draws := fs.String("draw", "0.3", "probability floor or mode; one value or three comma-separated draws")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *compression < gzip.BestSpeed || *compression > gzip.BestCompression {
		return fmt.Errorf("compression must be between 1 and 9")
	}
	bet, err := parseSelections(*bets, cfr.Streets)
	if err != nil {
		return fmt.Errorf("bet: %w", err)
	}
	draw, err := parseSelections(*draws, cfr.Streets-1)
	if err != nil {
		return fmt.Errorf("draw: %w", err)
	}
	if *in == "" || *out == "" {
		return fmt.Errorf("bp and out are required")
	}
	source, err := filepath.Abs(*in)
	if err != nil {
		return err
	}
	dest, err := filepath.Abs(*out)
	if err != nil {
		return err
	}
	if source == dest {
		return fmt.Errorf("output must differ from source")
	}
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("output already exists: %s", dest)
	} else if !os.IsNotExist(err) {
		return err
	}
	w := newWorld()
	if *basePath != "" && w.layout.FixedGroups <= 1 {
		return fmt.Errorf("base restoration requires a fixed-card layout")
	}
	pl, err := w.load(source, 0)
	if err != nil {
		return err
	}
	applySelections(w, pl.BP, bet, draw)
	if *fallbackPath != "" {
		fallback, err := w.load(*fallbackPath, 0)
		if err != nil {
			return err
		}
		if err := fillMissingPolicy(w, pl.BP, fallback.BP); err != nil {
			return err
		}
	}
	if *basePath != "" {
		base, err := w.load("base:"+*basePath, 0)
		if err != nil {
			return err
		}
		if err := restoreBasePolicy(pl.BP, base.BP); err != nil {
			return err
		}
	}
	f, err := os.CreateTemp(filepath.Dir(dest), ".selected-*.gz")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := pl.BP.EncodeLevel(f, *compression); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// A competing experiment may have created dest during compression.
	// Linking within this directory publishes atomically without replacing it.
	return os.Link(f.Name(), dest)
}

// fillMissingPolicy copies whole empty information sets. A zero within a
// learned set is intentional and must not inherit the fallback action.
func fillMissingPolicy(w *world, bp, fallback *cfr.Blueprint) error {
	if int64(len(bp.Bet)) != w.layout.BetSlots || int64(len(bp.Draw)) != w.layout.DrawSlots ||
		len(fallback.Bet) != len(bp.Bet) || len(fallback.Draw) != len(bp.Draw) {
		return fmt.Errorf("fallback policy must match the complete layout")
	}
	fill := func(dst, src []byte) {
		for _, p := range dst {
			if p != 0 {
				return
			}
		}
		copy(dst, src)
	}
	seen := make(map[int64]bool)
	for group := 0; group < w.layout.FixedGroups; group++ {
		for i := range w.tree.Nodes {
			node := &w.tree.Nodes[i]
			if node.Kind != cfr.KindBet || (group > 0 && !w.layout.Sliced(node, group)) {
				continue
			}
			start := w.layout.BetSlotFixed(node, 0, 0, group)
			if seen[start] {
				continue
			}
			seen[start] = true
			n := int64(len(node.Acts))
			end := start + int64(cfr.BetContexts(int(node.Street))*w.layout.Buckets(int(node.Street)))*n
			for slot := start; slot < end; slot += n {
				fill(bp.Bet[slot:slot+n], fallback.Bet[slot:slot+n])
			}
		}
	}
	for slot := int64(0); slot < w.layout.DrawSlots; slot += cfr.MaxCand {
		fill(bp.Draw[slot:slot+cfr.MaxCand], fallback.Draw[slot:slot+cfr.MaxCand])
	}
	return nil
}

func parseSelections(text string, count int) ([]selection, error) {
	parts := strings.Split(text, ",")
	if len(parts) != 1 && len(parts) != count {
		return nil, fmt.Errorf("expected one or %d selections", count)
	}
	out := make([]selection, count)
	for i := range out {
		part := strings.TrimSpace(parts[min(i, len(parts)-1)])
		if part == "mode" {
			out[i].greedy = true
			continue
		}
		p, err := strconv.ParseFloat(part, 64)
		if err != nil || math.IsNaN(p) || math.IsInf(p, 0) || p < 0 || p > 1 {
			return nil, fmt.Errorf("invalid selection %q", part)
		}
		out[i].floor = byte(p * 255)
	}
	return out, nil
}

func applySelections(w *world, bp *cfr.Blueprint, bets, draws []selection) {
	// Compact layouts share node offsets. Select each physical table once.
	seen := make(map[int64]bool)
	for group := 0; group < w.layout.FixedGroups; group++ {
		for i := range w.tree.Nodes {
			node := &w.tree.Nodes[i]
			if node.Kind != cfr.KindBet || (group > 0 && !w.layout.Sliced(node, group)) {
				continue
			}
			street, n := int(node.Street), int64(len(node.Acts))
			start := w.layout.BetSlotFixed(node, 0, 0, group)
			if seen[start] {
				continue
			}
			seen[start] = true
			end := start + int64(cfr.BetContexts(street)*w.layout.Buckets(street))*n
			for slot := start; slot < end; slot += n {
				selectSet(bp.Bet[slot:slot+n], bets[street])
			}
		}
	}
	// Derive draw offsets through the layout so fixed-card slices work too.
	clear(seen)
	for group := 0; group < w.layout.FixedGroups; group++ {
		for street := cfr.Draw1; street <= cfr.Draw3; street++ {
			for seat := 0; seat < 2; seat++ {
				for aggr := 0; aggr < 3; aggr++ {
					for ctx := 0; ctx < 16; ctx++ {
						start := w.layout.DrawSlotFixed(street, seat, aggr, ctx, 0, group)
						if seen[start] {
							continue
						}
						seen[start] = true
						end := start + int64(w.abs.NumDrawClasses*cfr.MaxCand)
						for slot := start; slot < end; slot += cfr.MaxCand {
							selectSet(bp.Draw[slot:slot+cfr.MaxCand], draws[street-cfr.Draw1])
						}
					}
				}
			}
		}
	}
}

func selectSet(probs []byte, mode selection) {
	best, total := 0, 0
	for i, p := range probs {
		if p > probs[best] {
			best = i
		}
		if p >= mode.floor {
			total += int(p)
		}
	}
	if len(probs) == 0 {
		return
	}
	if mode.greedy {
		trained := probs[best] > 0
		clear(probs)
		if trained {
			probs[best] = 255
		}
		return
	}
	if total == 0 {
		clear(probs)
		return
	}
	sum := 0
	for i, p := range probs {
		if p < mode.floor {
			probs[i] = 0
		} else {
			probs[i] = byte(int(p) * 255 / total)
		}
		sum += int(probs[i])
	}
	probs[best] += byte(255 - sum)
}

// restoreBasePolicy preserves the exact deployed policy in the base prefixes.
// The loader has already checked the base abstraction and layout dimensions.
func restoreBasePolicy(candidate, base *cfr.Blueprint) error {
	if len(base.Bet) > len(candidate.Bet) || len(base.Draw) > len(candidate.Draw) {
		return fmt.Errorf("base policy exceeds candidate dimensions")
	}
	copy(candidate.Bet, base.Bet)
	copy(candidate.Draw, base.Draw)
	return nil
}
