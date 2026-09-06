// Command cfrgen trains and evaluates the MCCFR blueprint.
//
//	cfrgen train -iters N -workers W -out internal/lapis/blueprint.bin.gz
//	cfrgen train -model cobalt -weight 0.5 ...   restricted response to a model
//	cfrgen eval  -bp FILE -vs cobalt|h3|FILE -hands N
//	cfrgen stats -bp FILE
//
// The blueprint is committed output: it is built here, not at build time.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/deuce"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: cfrgen train|eval|stats [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "train":
		err = train(os.Args[2:])
	case "eval":
		err = eval(os.Args[2:])
	case "stats":
		err = stats(os.Args[2:])
	case "probe":
		err = probe(os.Args[2:])
	case "refine":
		err = refine(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "cfrgen:", err)
		os.Exit(1)
	}
}

type world struct {
	tree   *cfr.Tree
	abs    *cfr.Abstraction
	layout *cfr.Layout
	eval   deuce.Table
}

func newWorld() *world {
	w := &world{tree: cfr.BuildTree(), abs: cfr.BuildAbstraction()}
	w.layout = cfr.NewLayout(w.tree, w.abs)
	w.eval = deuce.NewTable()
	return w
}

func (w *world) load(path string, purify float64) (*cfr.Player, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	bp, err := cfr.Decode(data, w.layout)
	if err != nil {
		return nil, err
	}
	return &cfr.Player{Tree: w.tree, Abs: w.abs, Layout: w.layout, BP: bp, Purify: purify}, nil
}

// model resolves an opponent name: a heuristic, or a blueprint file.
func (w *world) model(name string, purify float64) (cfr.Model, error) {
	if rest, sharp := strings.CutPrefix(name, "sharp:"); sharp {
		// sharp:ALPHA:FILE.json
		alphaText, path, ok := strings.Cut(rest, ":")
		if !ok || !strings.HasSuffix(path, ".json") {
			return nil, fmt.Errorf("sharp wants sharp:ALPHA:FILE.json")
		}
		alpha, err := strconv.ParseFloat(alphaText, 64)
		if err != nil || alpha <= 0 {
			return nil, fmt.Errorf("invalid sharpening %q", alphaText)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		m, err := cfr.DecodeEmpirical(raw)
		if err != nil {
			return nil, err
		}
		return m.Sharpened(alpha), nil
	}
	if path, mode := strings.CutPrefix(name, "mode:"); mode {
		if !strings.HasSuffix(path, ".json") {
			return nil, fmt.Errorf("mode requires an empirical JSON model")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		m, err := cfr.DecodeEmpirical(raw)
		if err != nil {
			return nil, err
		}
		return m.Mode(), nil
	}
	if strings.HasSuffix(name, ".json") {
		raw, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		return cfr.DecodeEmpirical(raw)
	}
	switch name {
	case "cobalt":
		return cfr.Heuristic{Cobalt: true}, nil
	case "h3":
		return cfr.Heuristic{}, nil
	case "":
		return nil, nil
	}
	return w.load(name, purify)
}

func train(args []string) error {
	fs := flag.NewFlagSet("train", flag.ContinueOnError)
	iters := fs.Int64("iters", 1_000_000, "hands to walk")
	workers := fs.Int("workers", 1, "concurrent walkers; use 1 for reproducibility")
	seed := fs.Uint64("seed", 1, "PCG seed")
	out := fs.String("out", "blueprint.bin.gz", "blueprint output path")
	every := fs.Duration("every", 10*time.Minute, "checkpoint interval")
	modelName := fs.String("model", "", "fixed opponent: cobalt, h3, blueprint, empirical JSON, or mode:FILE.json")
	weight := fs.Float64("weight", 0, "fraction of opponent decisions the model plays")
	modelScope := fs.String("model-scope", "decision", "sample the fixed model per decision or per hand")
	minVisits := fs.Uint("minvisits", 20, "prune sets visited fewer times than this")
	statePath := fs.String("state", "", "raw table checkpoint to write alongside the blueprint")
	resume := fs.String("resume", "", "raw table checkpoint to start from")
	resetAvg := fs.Bool("resetavg", false, "with -resume: drop the accumulated average, keep the regrets")
	regret := fs.String("regret", "plus", "regret update: plus or vanilla (signed)")
	cpuProfile := fs.String("cpuprofile", "", "write a CPU profile of the run")
	fixed := fs.String("fixed", "", "card dealt to the big blind every hand: e.g. Qh, or random")
	hidden := fs.Float64("hidden", 0.15, "with -fixed and a fixed-card layout: fraction of hands the button plays not knowing the card")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			return err
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			return err
		}
		defer pprof.StopCPUProfile()
	}

	if *iters <= 0 || *workers <= 0 || *every <= 0 {
		return fmt.Errorf("iters, workers and checkpoint interval must be positive")
	}
	if *regret != "plus" && *regret != "vanilla" {
		return fmt.Errorf("regret must be plus or vanilla")
	}
	if *modelScope != "decision" && *modelScope != "hand" {
		return fmt.Errorf("model-scope must be decision or hand")
	}
	if *weight < 0 || *weight > 1 || math.IsNaN(*weight) {
		return fmt.Errorf("weight must be between 0 and 1")
	}
	if *weight > 0 && *modelName == "" {
		return fmt.Errorf("positive weight requires an opponent model")
	}
	w := newWorld()
	model, err := w.model(*modelName, 0)
	if err != nil {
		return err
	}
	tr := cfr.NewTrainer(w.tree, w.abs, w.layout, w.eval)
	tr.Vanilla = *regret == "vanilla"
	tr.Model, tr.ModelWeight = model, *weight
	tr.ModelByHand = *modelScope == "hand"
	tr.FixedHidden = *hidden
	if *fixed == "random" {
		tr.FixedRandom = true
	} else if *fixed != "" {
		card, err := cards.ParseCard(*fixed)
		if err != nil {
			return err
		}
		tr.FixedBB, tr.HasFixedBB = card, true
	}
	if *resume != "" {
		if err := tr.LoadState(*resume); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "resumed at %d iterations\n", tr.Iterations())
		if *resetAvg {
			clear(tr.BetStrat)
			clear(tr.DrawStrat)
			clear(tr.BetVisits)
			clear(tr.DrawVisits)
		}
	}
	fmt.Fprintf(os.Stderr, "tables: %d bet slots, %d draw slots; model=%q weight=%.2f scope=%s regret=%s\n",
		w.layout.BetSlots, w.layout.DrawSlots, *modelName, *weight, *modelScope, *regret)

	done, stopped := make(chan struct{}), make(chan struct{})
	initialIterations := tr.Iterations()
	go func() {
		defer close(stopped)
		start := time.Now()
		ticker := time.NewTicker(*every)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				n := tr.Iterations()
				fmt.Fprintf(os.Stderr, "%s: %d iterations, %.0f/s\n", time.Since(start).Round(time.Second), n,
					float64(n-initialIterations)/time.Since(start).Seconds())
				if err := save(tr, *out, *statePath, uint32(*minVisits)); err != nil {
					fmt.Fprintln(os.Stderr, "checkpoint:", err)
				}
			}
		}
	}()
	start := time.Now()
	tr.Run(*iters, *workers, *seed)
	close(done)
	<-stopped
	fmt.Fprintf(os.Stderr, "done: %d new iterations (%d total) in %s\n", tr.Iterations()-initialIterations, tr.Iterations(), time.Since(start).Round(time.Second))
	return save(tr, *out, *statePath, uint32(*minVisits))
}

func save(tr *cfr.Trainer, path, statePath string, minVisits uint32) error {
	if statePath != "" {
		if err := tr.SaveState(statePath); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := tr.Extract(minVisits).Encode(f); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func eval(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	bpPath := fs.String("bp", "", "hero: cobalt, h3 or a blueprint file")
	vs := fs.String("vs", "cobalt", "opponent: cobalt, h3, blueprint, empirical JSON, or mode:FILE.json")
	hands := fs.Int("hands", 100_000, "decks to play, each twice")
	seed := fs.Uint64("seed", 7, "deal seed")
	purify := fs.Float64("purify", 0, "drop actions under this probability")
	greedy := fs.Bool("greedy", false, "hero uses the most likely trained action")
	fixed := fs.String("fixed", "", "card dealt to the big blind every hand: e.g. Qh, or random")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *bpPath == "" || *vs == "" || *hands < 2 {
		return fmt.Errorf("bp and vs are required, and hands must be at least 2")
	}
	if *purify < 0 || *purify > 1 || math.IsNaN(*purify) {
		return fmt.Errorf("purify must be between 0 and 1")
	}
	w := newWorld()
	hero, err := w.model(*bpPath, *purify)
	if err != nil {
		return err
	}
	if *greedy {
		player, ok := hero.(*cfr.Player)
		if !ok {
			return fmt.Errorf("greedy requires a blueprint hero")
		}
		player.Greedy = true
	}
	villain, err := w.model(*vs, *purify)
	if err != nil {
		return err
	}
	var fixedCard *cards.Card
	if *fixed != "" && *fixed != "random" {
		card, err := cards.ParseCard(*fixed)
		if err != nil {
			return err
		}
		fixedCard = &card
	}
	result := cfr.SimulateFixed(w.tree, w.eval, hero, villain, cfr.Heuristic{}, *hands, *seed, fixedCard, *fixed == "random")
	fmt.Printf("%s vs %s: %+.2f ±%.2f BB/100 over %d hands\n",
		*bpPath, *vs, result.Rate, result.CI, result.Hands)
	fmt.Printf("hero fallbacks: %d/%d decisions; opponent: %d/%d\n",
		result.Fallbacks[0], result.Decisions[0], result.Fallbacks[1], result.Decisions[1])
	return nil
}

func stats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	bpPath := fs.String("bp", "", "blueprint")
	if err := fs.Parse(args); err != nil {
		return err
	}
	w := newWorld()
	pl, err := w.load(*bpPath, 0)
	if err != nil {
		return err
	}
	var lines []string
	for street := 0; street < cfr.Streets; street++ {
		sets, trained := 0, 0
		for i := range w.tree.Nodes {
			node := &w.tree.Nodes[i]
			if node.Kind != cfr.KindBet || int(node.Street) != street {
				continue
			}
			n := int64(len(node.Acts))
			count := int64(cfr.BetContexts(street)) * int64(w.layout.Buckets(street))
			for set := int64(0); set < count; set++ {
				sets++
				slot := node.Offset + set*n
				if pl.BP.Bet[slot] != 0 || pl.BP.Bet[slot+1] != 0 {
					trained++
				}
			}
		}
		lines = append(lines, fmt.Sprintf("street %d: %d/%d betting sets trained", street, trained, sets))
	}
	sets, trained := 0, 0
	for slot := int64(0); slot < w.layout.DrawSlots; slot += cfr.MaxCand {
		sets++
		if pl.BP.Draw[slot] != 0 || pl.BP.Draw[slot+1] != 0 {
			trained++
		}
	}
	lines = append(lines, fmt.Sprintf("draws: %d/%d sets trained", trained, sets))
	fmt.Println(strings.Join(lines, "\n"))
	return nil
}

// probe prints the blueprint's predraw mix for a hand: opening on the
// button, and defending the big blind against a raise.
func probe(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	bpPath := fs.String("bp", "", "blueprint")
	handText := fs.String("hand", "7c 5d 4h 3s 2c", "five cards")
	if err := fs.Parse(args); err != nil {
		return err
	}
	w := newWorld()
	pl, err := w.load(*bpPath, 0)
	if err != nil {
		return err
	}
	hand, err := cards.Parse(strings.Fields(*handText))
	if err != nil {
		return err
	}
	var view cfr.View
	copy(view.Hand[:], cards.SortedByRank(hand))
	view.LastAggr = -1
	for p := range view.Drawn {
		for s := range view.Drawn[p] {
			view.Drawn[p][s] = -1
		}
	}
	root := &w.tree.Nodes[w.tree.Root]
	view.Seat, view.Node = cfr.Btn, w.tree.Root
	fmt.Printf("button opening [fold call raise]: %v\n", pl.Probabilities(&view))
	view.Seat, view.Node = cfr.BB, root.Next[2]
	view.LastAggr = cfr.Btn
	fmt.Printf("big blind vs raise [fold call raise]: %v\n", pl.Probabilities(&view))
	view.Seat, view.Node = cfr.BB, root.Next[1]
	view.LastAggr = -1
	fmt.Printf("big blind vs limp [check raise]: %v\n", pl.Probabilities(&view))
	return nil
}
