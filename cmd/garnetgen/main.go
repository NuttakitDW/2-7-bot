// Command garnetgen builds Garnet's offline EV ranking and predraw policy.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/nuttakit/2-7-bot/internal/beryl"
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/garnet"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/lapis"
	"github.com/nuttakit/2-7-bot/internal/sixmaxsim"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: garnetgen <rank|train> [flags]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "rank":
		rankMain(os.Args[2:])
	case "train":
		trainMain(os.Args[2:])
	default:
		fmt.Fprintln(os.Stderr, "usage: garnetgen <rank|train> [flags]")
		os.Exit(2)
	}
}

func rankMain(args []string) {
	flags := flag.NewFlagSet("rank", flag.ExitOnError)
	out := flags.String("out", "bin/garnet/ranking.json", "final ranking or pilot checkpoint JSON")
	seed := flags.Uint64("seed", 27091401, "reproducible root seed")
	samples := flags.Int("samples-per-position", 64, "paired call/raise samples per class and position")
	start := flags.Int("start", 0, "first feasible class ordinal")
	limit := flags.Int("limit", 0, "number of feasible classes; 0 means all")
	workers := flags.Int("workers", 1, "classes evaluated concurrently")
	referenceHash := flags.String("reference-hash", "", "SHA-256 of frozen Spinel reference policy")
	referenceCodeHash := flags.String("reference-code-hash", "", "SHA-256 of frozen Beryl/Onyx continuation sources")
	if err := flags.Parse(args); err != nil {
		fatal(err)
	}
	if *samples <= 0 || *start < 0 || *limit < 0 || *workers <= 0 || len(*referenceHash) != 64 || len(*referenceCodeHash) != 64 {
		fatal(fmt.Errorf("samples/start/limit or reference hash is invalid"))
	}
	model, err := lapis.LoadSpinelModel()
	if err != nil {
		fatal(err)
	}
	if err := validateReferencePolicyHash(*referenceHash); err != nil {
		fatal(err)
	}
	if err := runRank(*out, *seed, *samples, *start, *limit, *workers, *referenceHash, *referenceCodeHash, model); err != nil {
		fatal(err)
	}
}

func validateReferencePolicyHash(supplied string) error {
	embedded, err := lapis.EmbeddedPolicyHash()
	if err != nil {
		return err
	}
	if supplied != embedded {
		return fmt.Errorf("reference hash %q does not match embedded Spinel policy hash %q", supplied, embedded)
	}
	return nil
}

type rankCheckpoint struct {
	Version             int                 `json:"version"`
	Seed                uint64              `json:"seed"`
	SamplesPerPosition  int                 `json:"samples_per_position"`
	ReferencePolicyHash string              `json:"reference_policy_hash"`
	ReferenceCodeHash   string              `json:"reference_code_hash"`
	EngineHash          string              `json:"engine_hash"`
	ContextMix          string              `json:"context_mix"`
	Stats               garnet.RankingStats `json:"stats"`
	Classes             []garnet.ClassEV    `json:"classes"`
	Complete            bool                `json:"complete"`
	ElapsedSeconds      float64             `json:"elapsed_seconds"`
}

func runRank(path string, seed uint64, samples, start, limit, workers int, referenceHash, referenceCodeHash string, model cfr.Model) error {
	ids := feasibleClasses()
	if start >= len(ids) {
		return fmt.Errorf("start %d outside %d feasible classes", start, len(ids))
	}
	end := len(ids)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	checkpoint := rankCheckpoint{Version: garnet.RankingVersion, Seed: seed, SamplesPerPosition: samples,
		ReferencePolicyHash: referenceHash, ReferenceCodeHash: referenceCodeHash, EngineHash: "sixmaxsim-v1",
		ContextMix: "equal six-position mean; per-position natural Beryl-reference prefix conditional on paired legal call+raise first decisions"}
	started := time.Now()
	type job struct {
		ordinal int
		id      handclass.ID
	}
	type result struct {
		class garnet.ClassEV
		stats garnet.RankingStats
		err   error
	}
	jobs, results := make(chan job), make(chan result, workers)
	var wg sync.WaitGroup
	workers = min(workers, end-start)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for work := range jobs {
				class, stats, err := estimateClass(work.id, samples, seed+uint64(work.ordinal)*0x9E3779B97F4A7C15, model)
				results <- result{class, stats, err}
			}
		}()
	}
	go func() {
		for ordinal := start; ordinal < end; ordinal++ {
			jobs <- job{ordinal, ids[ordinal]}
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	for result := range results {
		if result.err != nil {
			return result.err
		}
		checkpoint.Classes = append(checkpoint.Classes, result.class)
		addStats(&checkpoint.Stats, result.stats)
		checkpoint.ElapsedSeconds = time.Since(started).Seconds()
		if len(checkpoint.Classes)%32 == 0 {
			if err := writeJSON(path, checkpoint); err != nil {
				return err
			}
		}
	}
	sort.Slice(checkpoint.Classes, func(i, j int) bool { return checkpoint.Classes[i].ID < checkpoint.Classes[j].ID })
	checkpoint.Complete = start == 0 && end == len(ids)
	checkpoint.ElapsedSeconds = time.Since(started).Seconds()
	if !checkpoint.Complete {
		return writeJSON(path, checkpoint)
	}
	ranking := &garnet.Ranking{Version: garnet.RankingVersion, Seed: seed, ReferencePolicyHash: referenceHash,
		ReferenceCodeHash: referenceCodeHash, SamplesPerPosition: samples,
		EngineHash: checkpoint.EngineHash, ContextMix: checkpoint.ContextMix, PositionAveraged: true,
		Stats: checkpoint.Stats, Classes: checkpoint.Classes}
	raw, err := garnet.EncodeRanking(ranking)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func feasibleClasses() []handclass.ID {
	ids := make([]handclass.ID, 0, handclass.Num)
	for id := handclass.ID(0); id < handclass.Num; id++ {
		if handclass.Weight(id) > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

type moments struct {
	n    uint64
	mean float64
	m2   float64
}

func (m *moments) add(value float64) {
	m.n++
	delta := value - m.mean
	m.mean += delta / float64(m.n)
	m.m2 += delta * (value - m.mean)
}

func (m moments) estimate() garnet.Estimate {
	se := 0.0
	if m.n > 1 {
		se = math.Sqrt(m.m2 / float64(m.n-1) / float64(m.n))
	}
	return garnet.Estimate{Mean: m.mean, Count: m.n, SE: se}
}

func estimateClass(id handclass.ID, samples int, seed uint64, model cfr.Model) (garnet.ClassEV, garnet.RankingStats, error) {
	rng := rand.New(rand.NewPCG(seed, seed^0xD1B54A32D192ED03))
	var calls, raises [garnet.Positions]moments
	var stats garnet.RankingStats
	for position := garnet.Position(0); position < garnet.Positions; position++ {
		maxAttempts := samples * 40
		for attempt := 0; calls[position].n < uint64(samples) && attempt < maxAttempts; attempt++ {
			hand := samplePhysicalClass(id, rng)
			branchSeed := seed + uint64(position+1)*0xD1B54A32D192ED03 + uint64(attempt)
			game, err := sixmaxsim.New(sixmaxsim.Config{Seed: branchSeed, HandNo: uint64(attempt),
				Forced: &sixmaxsim.ForcedHand{Seat: int(position), Cards: hand}})
			if err != nil {
				return garnet.ClassEV{}, stats, err
			}
			for !game.Done() && game.Street == sixmaxsim.Predraw && game.Actor != int(position) {
				seat, decision, _ := game.Legal()
				action, err := referenceDecision(game, seat, decision, model, branchSeed*31)
				if err != nil {
					return garnet.ClassEV{}, stats, err
				}
				if err := game.Apply(action); err != nil {
					return garnet.ClassEV{}, stats, err
				}
			}
			context := contextOf(game)
			if game.Done() || game.Street != sixmaxsim.Predraw || game.Actor != int(position) {
				stats.DecisionSkips[position][context]++
				continue
			}
			_, decision, _ := game.Legal()
			if decision.Call == nil || decision.Raise == nil {
				stats.DecisionSkips[position][context]++
				continue
			}
			// Folding can win no future chips. Its settled net is exactly the
			// negative prefix contribution, so no third continuation is needed.
			foldEV := -int64(game.Total[position])
			callEV, err := branchPayoff(game, int(position), wire.Call(), model, branchSeed*101)
			if err != nil {
				return garnet.ClassEV{}, stats, err
			}
			raiseEV, err := branchPayoff(game, int(position), wire.Raise(decision.Raise.MinTo), model, branchSeed*101)
			if err != nil {
				return garnet.ClassEV{}, stats, err
			}
			calls[position].add(float64(callEV - foldEV))
			raises[position].add(float64(raiseEV - foldEV))
			stats.PairedSamples[position][context]++
		}
		if calls[position].n != uint64(samples) {
			return garnet.ClassEV{}, stats, fmt.Errorf("position %d reached only %d/%d paired first decisions", position, calls[position].n, samples)
		}
	}
	callEstimate, raiseEstimate := stratifiedEstimate(calls[:]), stratifiedEstimate(raises[:])
	return garnet.ClassEV{ID: uint16(id), Weight: handclass.Weight(id), Call: callEstimate, Raise: raiseEstimate,
		BestEV: max(callEstimate.Mean, raiseEstimate.Mean)}, stats, nil
}

func stratifiedEstimate(groups []moments) garnet.Estimate {
	var estimate garnet.Estimate
	var variance float64
	for _, group := range groups {
		part := group.estimate()
		estimate.Mean += part.Mean / float64(len(groups))
		estimate.Count += part.Count
		variance += part.SE * part.SE
	}
	estimate.SE = math.Sqrt(variance) / float64(len(groups))
	return estimate
}

func branchPayoff(prefix *sixmaxsim.Game, hero int, action wire.Action, model cfr.Model, seed uint64) (int64, error) {
	game := prefix.Clone()
	if err := game.Apply(action); err != nil {
		return 0, err
	}
	if !game.Done() {
		if err := game.Continue(referenceFactory(model, seed)); err != nil {
			return 0, err
		}
	}
	payoffs, ok := game.Payoffs()
	if !ok {
		return 0, fmt.Errorf("continuation did not settle")
	}
	return payoffs[hero], nil
}

func referenceDecision(game *sixmaxsim.Game, seat int, decision wire.Decision, model cfr.Model, seed uint64) (wire.Action, error) {
	player, err := referenceFactory(model, seed)(seat, game.Seed)
	if err != nil {
		return wire.Action{}, err
	}
	initializePlayer(player, game, seat)
	return player.Decide(decision), nil
}

func referenceFactory(model cfr.Model, root uint64) sixmaxsim.Factory {
	return func(seat int, seed uint64) (sixmaxsim.Player, error) {
		return beryl.NewDeterministic(model, root^seed^uint64(seat+1)*0x9E3779B97F4A7C15)
	}
}

func initializePlayer(player sixmaxsim.Player, game *sixmaxsim.Game, seat int) {
	cap := uint8(sixmaxsim.RaiseCap)
	player.Hello(wire.Message{Type: wire.MsgHello, Proto: 1, GameID: "27td-fl", SeatCount: sixmaxsim.Seats,
		StartingStack: sixmaxsim.StartingStack, Stakes: wire.Stakes{Kind: "blinds", SmallBlind: sixmaxsim.SmallBlind, BigBlind: sixmaxsim.BigBlind},
		Betting: wire.Betting{Kind: "fixed-limit", RaiseCap: &cap}})
	player.HandStart(wire.Message{Type: wire.MsgHandStart, HandNo: game.HandNo, Seat: seat})
	for _, event := range game.EventsFor(seat) {
		player.Observe(event)
	}
}

func contextOf(game *sixmaxsim.Game) garnet.Context {
	raises, calls := game.Wagers-1, 0
	for _, event := range game.Events {
		if event.Kind == wire.EventActed && event.Street == sixmaxsim.Predraw && event.Action.Kind == wire.ActionCall {
			calls++
		}
	}
	switch {
	case raises == 0 && calls == 0:
		return garnet.Unopened
	case raises == 0:
		return garnet.Limped
	case raises == 1:
		return garnet.SingleOpen
	default:
		return garnet.MultipleOpen
	}
}

func samplePhysicalClass(id handclass.ID, rng *rand.Rand) []cards.Card {
	representative := handclass.Representative(id)
	ranks := make([]cards.Rank, len(representative))
	for i, card := range representative {
		ranks[i] = card.Rank
	}
	suits := [...]cards.Suit{cards.Clubs, cards.Diamonds, cards.Hearts, cards.Spades}
	if cards.SameSuit(representative) {
		suit := suits[rng.IntN(len(suits))]
		for i, rank := range ranks {
			representative[i] = cards.Card{Rank: rank, Suit: suit}
		}
		return representative
	}
	for {
		hand := make([]cards.Card, len(ranks))
		for start := 0; start < len(ranks); {
			end := start + 1
			for end < len(ranks) && ranks[end] == ranks[start] {
				end++
			}
			permutation := rng.Perm(len(suits))
			for i := start; i < end; i++ {
				hand[i] = cards.Card{Rank: ranks[i], Suit: suits[permutation[i-start]]}
			}
			start = end
		}
		if !cards.SameSuit(hand) {
			return hand
		}
	}
}

func addStats(dst *garnet.RankingStats, src garnet.RankingStats) {
	for position := garnet.Position(0); position < garnet.Positions; position++ {
		for context := garnet.Context(0); context < garnet.Contexts; context++ {
			dst.PairedSamples[position][context] += src.PairedSamples[position][context]
			dst.DecisionSkips[position][context] += src.DecisionSkips[position][context]
		}
	}
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func dir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
