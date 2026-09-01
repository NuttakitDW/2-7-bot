// Command chartgen derives the predraw open/defend chart from equity
// rollouts and writes it as generated Go source (internal/policy's
// chart_table.go).
//
// The method, in the order it runs:
//
//  1. Ranking pass: every live hand class gets a showdown equity against a
//     uniform villain (internal/equity). Rankings, not absolute equities,
//     are the load-bearing output — orderings are robust to the unknown
//     opponent range in a way absolute numbers are not.
//  2. Fixed point: the big blind defends where realized equity against the
//     button's opening range clears its pot odds (call 1 small bet into a
//     pot of 3 → 25%); the button opens where the raise's EV against that
//     defending range beats forfeiting the posted small blind. Each range
//     is recomputed against the other until membership stops moving.
//
// The one free parameter is the realization factor r: raw showdown equity
// overstates what a hand banks across three betting streets, so equity is
// scaled by r before it faces a threshold. r is chosen by measurement — the
// generated candidates play h1 head-to-head — not by argument.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/equity"
	"github.com/nuttakit/2-7-bot/internal/handclass"
)

// The predraw pot geometry, in small bets. The button completes 1.5 to
// raise; a fold forfeits the 0.5 already posted, so EV is measured against
// that as zero. The big blind calls 1 into blinds-plus-raise of 3.
const (
	blindsPot    = 1.5 // what an uncontested open wins
	openCost     = 1.5 // the button's price beyond its posted small blind
	calledPot    = 4.0 // pot after the big blind calls the open
	defendCost   = 1.0 // the big blind's price to continue
	defendEquity = defendCost / calledPot
	openBit      = 1
	defendBit    = 2
)

func main() {
	samples := flag.Int("samples", 20000, "ranking-pass rollouts per class")
	iter := flag.Int("iter", 6000, "rollouts per class per fixed-point round")
	realization := flag.Float64("r", 0.85, "equity realization factor")
	seed := flag.Int64("seed", 1, "rollout seed")
	rounds := flag.Int("rounds", 8, "fixed-point round cap")
	out := flag.String("out", "internal/policy/chart_table.go", "generated file path")
	flag.Parse()

	start := time.Now()
	gen := newGenerator(*seed, *realization)

	log.Printf("ranking pass: %d classes x %d samples", len(gen.live), *samples)
	uniform := gen.equities(nil, *samples, 0)

	open := gen.threshold(uniform, 0.35) // a deliberately wide start
	var defend []bool
	churn := math.Inf(1)
	round := 0
	for ; round < *rounds; round++ {
		vsOpen := gen.equities(member(open), *iter, 100+round)
		nextDefend := gen.defends(vsOpen)

		vsDefend := gen.equities(member(nextDefend), *iter, 200+round)
		nextOpen := gen.opens(vsDefend, gen.fraction(nextDefend))

		churn = gen.fraction(diff(open, nextOpen)) + gen.fraction(diffOrNew(defend, nextDefend))
		open, defend = nextOpen, nextDefend
		log.Printf("round %d: open %.1f%%, defend %.1f%%, churn %.2f%%",
			round+1, 100*gen.fraction(open), 100*gen.fraction(defend), 100*churn)
		if churn < 0.003 {
			round++
			break
		}
	}

	openFrac, defendFrac := gen.fraction(open), gen.fraction(defend)
	vpip := (openFrac + openFrac*defendFrac) / 2
	log.Printf("button open %.1f%%, big-blind defend %.1f%%, predicted vpip %.1f%%",
		100*openFrac, 100*defendFrac, 100*vpip)
	if vpip < 0.55 || vpip > 0.80 {
		log.Printf("WARNING: predicted vpip %.1f%% is outside the rivals' 63-66%% neighborhood", 100*vpip)
	}

	meta := fmt.Sprintf(
		"seed=%d samples=%d iter=%d r=%.2f rounds=%d churn=%.2f%%\n"+
			"// button open %.1f%% of deals, big-blind defend %.1f%%, predicted vpip %.1f%%",
		*seed, *samples, *iter, *realization, round, 100*churn,
		100*openFrac, 100*defendFrac, 100*vpip)
	if err := writeTable(*out, meta, open, defend); err != nil {
		log.Fatalf("writing %s: %v", *out, err)
	}
	log.Printf("wrote %s in %s", *out, time.Since(start).Round(time.Second))
}

// generator holds what every pass shares: the live classes, their
// representatives and weights.
type generator struct {
	seed        int64
	realization float64
	live        []handclass.ID
	reps        [][]cards.Card
	weights     []float64
	totalWeight float64
}

func newGenerator(seed int64, realization float64) *generator {
	gen := &generator{seed: seed, realization: realization}
	for id := handclass.ID(0); id < handclass.Num; id++ {
		weight := handclass.Weight(id)
		if weight == 0 {
			continue
		}
		gen.live = append(gen.live, id)
		gen.reps = append(gen.reps, handclass.Representative(id))
		gen.weights = append(gen.weights, float64(weight))
		gen.totalWeight += float64(weight)
	}
	return gen
}

// equities runs one rollout pass for every live class, in parallel. The
// per-class seed folds in the pass number so every pass is independent yet
// reproducible.
func (g *generator) equities(opp equity.Accept, samples, pass int) []float64 {
	out := make([]float64, len(g.live))
	var next int64
	var wg sync.WaitGroup
	var mu sync.Mutex
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				i := int(next)
				next++
				mu.Unlock()
				if i >= len(g.live) {
					return
				}
				classSeed := g.seed + int64(g.live[i])*1_000_003 + int64(pass)*777_767_777
				out[i] = equity.Showdown(g.reps[i], opp, samples, classSeed)
			}
		}()
	}
	wg.Wait()
	return out
}

// threshold marks the classes at or above an equity floor.
func (g *generator) threshold(equities []float64, floor float64) []bool {
	set := make([]bool, len(g.live))
	for i, eq := range equities {
		set[i] = eq >= floor
	}
	return set
}

// defends applies the big blind's pot odds to realized equity against the
// opening range.
func (g *generator) defends(vsOpen []float64) []bool {
	set := make([]bool, len(g.live))
	for i, eq := range vsOpen {
		set[i] = g.realization*eq >= defendEquity
	}
	return set
}

// opens marks the classes whose raise EV against the defending range beats
// folding the button. One bet deep: the defender folds often enough that
// modeling the reraise war would change little and cost a parameter.
func (g *generator) opens(vsDefend []float64, defendFraction float64) []bool {
	set := make([]bool, len(g.live))
	for i, eq := range vsDefend {
		ev := (1-defendFraction)*blindsPot +
			defendFraction*(g.realization*eq*calledPot-openCost)
		set[i] = ev > 0
	}
	return set
}

// fraction is a set's share of all deals, by class weight.
func (g *generator) fraction(set []bool) float64 {
	total := 0.0
	for i, in := range set {
		if in {
			total += g.weights[i]
		}
	}
	return total / g.totalWeight
}

// member turns a live-index set into an Accept over class IDs.
func member(set []bool) equity.Accept {
	byID := make([]bool, handclass.Num)
	index := 0
	for id := handclass.ID(0); id < handclass.Num; id++ {
		if handclass.Weight(id) == 0 {
			continue
		}
		byID[id] = set[index]
		index++
	}
	return func(id handclass.ID) bool { return byID[id] }
}

// diff is the symmetric difference of two sets.
func diff(a, b []bool) []bool {
	out := make([]bool, len(b))
	for i := range b {
		out[i] = a[i] != b[i]
	}
	return out
}

// diffOrNew treats a nil previous set as all-different, so the first round
// reports full churn rather than crashing.
func diffOrNew(a, b []bool) []bool {
	if a == nil {
		return b
	}
	return diff(a, b)
}

// writeTable renders the generated source. Two bits per class; dead classes
// stay zero.
func writeTable(path, meta string, open, defend []bool) error {
	table := make([]uint8, handclass.Num)
	index := 0
	for id := handclass.ID(0); id < handclass.Num; id++ {
		if handclass.Weight(id) == 0 {
			continue
		}
		if open[index] {
			table[id] |= openBit
		}
		if defend[index] {
			table[id] |= defendBit
		}
		index++
	}

	var buf []byte
	buf = append(buf, "// Code generated by cmd/chartgen. DO NOT EDIT.\n//\n// "...)
	buf = append(buf, meta...)
	buf = append(buf, "\npackage policy\n\n"+
		"import \"github.com/nuttakit/2-7-bot/internal/handclass\"\n\n"+
		"// chartTable holds two bits per predraw hand class: bit 0 opens the\n"+
		"// button, bit 1 defends the big blind. See cmd/chartgen for the method.\n"+
		"var chartTable = [handclass.Num]uint8{"...)
	for i, entry := range table {
		if i%64 == 0 {
			buf = append(buf, "\n\t"...)
		}
		buf = append(buf, fmt.Sprintf("%d, ", entry)...)
	}
	buf = append(buf, "\n}\n"...)
	return os.WriteFile(path, buf, 0o644)
}
