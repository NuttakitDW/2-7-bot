// Command drawgen derives the draw table from branched equity rollouts and
// writes it as generated Go source (internal/policy's draw_table.go).
//
// The method: roll out full deals with both seats playing the current draw
// rule, and at every hero draw decision branch into each candidate keep
// (policy.DrawCandidates), playing all of them to showdown against the same
// villain (equity.DrawSamples). Aggregating those paired outcomes per
// (hand class, street × read context, candidate) and taking the best mean
// gives one measured keep per cell. Cells too rare to measure stay
// undecided and the runtime falls back to the structural rule.
//
// The pass is then repeated with both seats playing the previous pass's
// table — a fixed-point iteration in cmd/chartgen's mold — until cell
// membership stops moving. There is no realization factor and no free
// parameter: the objective is showdown equity on the real information
// timeline, and the pre-history that produced every context is played out
// rather than assumed. What the objective leaves out — betting pressure on
// marginal pat hands — is the judge's to catch, not a knob's to guess.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/equity"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/policy"
)

// noData mirrors internal/policy's drawNoData, the same way chartgen
// mirrors the chart bits.
const noData = 0xFF

const cells = int(handclass.Num) * policy.NumDrawContexts

func main() {
	deals := flag.Int("deals", 6_000_000, "deals rolled out per pass")
	minCount := flag.Int("min", 200, "samples a cell needs before it is decided")
	seed := flag.Int64("seed", 1, "rollout seed")
	rounds := flag.Int("rounds", 3, "fixed-point round cap")
	out := flag.String("out", "internal/policy/draw_table.go", "generated file path")
	flag.Parse()

	start := time.Now()
	var table []uint8
	var agg *tally
	churn := 1.0
	round := 0
	for ; round < *rounds; round++ {
		agg = pass(ruleFrom(table), *deals, *seed, round)
		next := decide(agg, *minCount)
		churn = agg.churn(table, next)
		table = next
		decided, coverage := agg.coverage(table)
		log.Printf("round %d: %d/%d cells decided, %.1f%% of samples covered, churn %.2f%%",
			round+1, decided, cells, 100*coverage, 100*churn)
		if churn < 0.003 {
			round++
			break
		}
	}

	mix := agg.mix(table)
	log.Printf("decision mix by sample weight: pat %.1f%%, structural %.1f%%, alternates %.1f%%",
		100*mix[0], 100*mix[1], 100*mix[2])

	decided, coverage := agg.coverage(table)
	meta := fmt.Sprintf(
		"deals=%d min=%d seed=%d rounds=%d churn=%.2f%%\n"+
			"// %d/%d cells decided covering %.1f%% of sampled decisions;\n"+
			"// by sample weight: pat %.1f%%, structural keep %.1f%%, alternate keeps %.1f%%",
		*deals, *minCount, *seed, round, 100*churn,
		decided, cells, 100*coverage, 100*mix[0], 100*mix[1], 100*mix[2])
	if err := writeTable(*out, meta, table); err != nil {
		log.Fatalf("writing %s: %v", *out, err)
	}
	log.Printf("wrote %s in %s", *out, time.Since(start).Round(time.Second))
}

// tally accumulates branched outcomes per (cell, candidate). Wins are
// doubled so ties stay integral.
type tally struct {
	winsX2 []uint32
	count  []uint32
}

func newTally() *tally {
	n := cells * policy.MaxDrawCandidates
	return &tally{winsX2: make([]uint32, n), count: make([]uint32, n)}
}

func (t *tally) add(sample equity.DrawSample) {
	cell := int(sample.Class)*policy.NumDrawContexts + policy.DrawContext(sample.Street, sample.Read)
	base := cell * policy.MaxDrawCandidates
	for k, outcome := range sample.Outcomes {
		t.winsX2[base+k] += uint32(2 * outcome)
		t.count[base+k]++
	}
}

func (t *tally) merge(other *tally) {
	for i := range t.winsX2 {
		t.winsX2[i] += other.winsX2[i]
		t.count[i] += other.count[i]
	}
}

// pass rolls out one round of deals across every CPU. Worker seeds fold in
// the worker index and the round, so every pass is independent yet
// reproducible — chartgen's scheme.
func pass(rule equity.DrawRule, deals int, seed int64, round int) *tally {
	workers := runtime.NumCPU()
	if workers > deals {
		workers = 1
	}
	per := deals / workers

	tallies := make([]*tally, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			n := per
			if w == 0 {
				n += deals % workers
			}
			local := newTally()
			workerSeed := seed + int64(w)*1_000_003 + int64(round)*777_767_777
			equity.DrawSamples(rule, n, workerSeed, local.add)
			tallies[w] = local
		}(w)
	}
	wg.Wait()

	total := tallies[0]
	for _, other := range tallies[1:] {
		total.merge(other)
	}
	return total
}

// decide picks each cell's best-mean candidate, leaving cells below the
// sample floor undecided. Ties go to the lower index — the pat, then the
// structural keep — so noise never prefers the exotic option.
//
// One structural line survives the measurement: a Broken-category class —
// a pair, straight or flush — never stands pat. Standing on a dead hand is
// a snow, and h3 does not snow, for h1's documented reasons (draw.go): the
// betting rules never cash the false read in, and honest draw counts are
// part of what keeps the bot legible. Without the line, sample noise near
// the floor elects a few hundred accidental snows.
func decide(t *tally, minCount int) []uint8 {
	table := make([]uint8, cells)
	for cell := 0; cell < cells; cell++ {
		class := handclass.ID(cell / policy.NumDrawContexts)
		broken := handclass.Weight(class) > 0 &&
			deuce.Categorize(handclass.Representative(class)) == deuce.Broken
		base := cell * policy.MaxDrawCandidates
		best, bestMean := noData, -1.0
		for k := 0; k < policy.MaxDrawCandidates; k++ {
			if k == 0 && broken {
				continue
			}
			if int(t.count[base+k]) < minCount {
				continue
			}
			mean := float64(t.winsX2[base+k]) / float64(2*t.count[base+k])
			if mean > bestMean {
				best, bestMean = k, mean
			}
		}
		table[cell] = uint8(best)
	}
	return table
}

// ruleFrom plays a candidate table, falling back to the compiled-in rule
// for undecided cells. A nil table is the first round: the current rule
// throughout.
func ruleFrom(table []uint8) equity.DrawRule {
	if table == nil {
		return nil
	}
	return func(hand []cards.Card, street int, read policy.Read) []cards.Card {
		cell := int(handclass.Of(hand))*policy.NumDrawContexts + policy.DrawContext(street, read)
		if entry := table[cell]; entry != noData {
			if candidates := policy.DrawCandidates(hand); int(entry) < len(candidates) {
				return policy.Discards(hand, candidates[entry])
			}
		}
		return policy.DrawDiscards(hand, street, read)
	}
}

// churn is the sample-weighted share of decisions that moved between two
// tables, the fixed point's stopping metric.
func (t *tally) churn(prev, next []uint8) float64 {
	if prev == nil {
		return 1
	}
	moved, total := 0.0, 0.0
	for cell := 0; cell < cells; cell++ {
		weight := float64(t.count[cell*policy.MaxDrawCandidates])
		total += weight
		if prev[cell] != next[cell] {
			moved += weight
		}
	}
	if total == 0 {
		return 0
	}
	return moved / total
}

// coverage reports how many cells are decided and the share of sampled
// decisions they cover.
func (t *tally) coverage(table []uint8) (decided int, share float64) {
	covered, total := 0.0, 0.0
	for cell := 0; cell < cells; cell++ {
		weight := float64(t.count[cell*policy.MaxDrawCandidates])
		total += weight
		if table[cell] != noData {
			decided++
			covered += weight
		}
	}
	if total == 0 {
		return decided, 0
	}
	return decided, covered / total
}

// mix is the sample-weighted split of decided cells between standing pat,
// the structural keep, and everything else.
func (t *tally) mix(table []uint8) [3]float64 {
	var weights [3]float64
	total := 0.0
	for cell := 0; cell < cells; cell++ {
		entry := table[cell]
		if entry == noData {
			continue
		}
		weight := float64(t.count[cell*policy.MaxDrawCandidates])
		total += weight
		switch entry {
		case 0:
			weights[0] += weight
		case 1:
			weights[1] += weight
		default:
			weights[2] += weight
		}
	}
	if total == 0 {
		return weights
	}
	for i := range weights {
		weights[i] /= total
	}
	return weights
}

// writeTable renders the generated source, chartgen's emitter shape: one
// row-major uint8 per (class, context).
func writeTable(path, meta string, table []uint8) error {
	var buf []byte
	buf = append(buf, "// Code generated by cmd/drawgen. DO NOT EDIT.\n//\n// "...)
	buf = append(buf, meta...)
	buf = append(buf, "\npackage policy\n\n"+
		"import \"github.com/nuttakit/2-7-bot/internal/handclass\"\n\n"+
		"// drawTable holds one candidate index per (hand class, draw context) —\n"+
		"// row-major, NumDrawContexts columns — into DrawCandidates of the held\n"+
		"// hand. drawNoData defers to the structural rule. See cmd/drawgen.\n"+
		"var drawTable = [handclass.Num * NumDrawContexts]uint8{"...)
	for i, entry := range table {
		if i%64 == 0 {
			buf = append(buf, "\n\t"...)
		}
		buf = append(buf, fmt.Sprintf("%d, ", entry)...)
	}
	buf = append(buf, "\n}\n"...)
	return os.WriteFile(path, buf, 0o644)
}
