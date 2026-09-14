package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nuttakit/2-7-bot/internal/garnet"
	"github.com/nuttakit/2-7-bot/internal/lapis"
)

func trainMain(args []string) {
	flags := flag.NewFlagSet("train", flag.ExitOnError)
	rankingPath := flags.String("ranking", "bin/garnet/ranking.json", "complete EV ranking JSON")
	out := flags.String("out", "bin/garnet/policy.json", "trained predraw policy JSON")
	iterations := flags.Uint64("iters", 0, "external-sampling CFR iterations")
	buckets := flags.Int("buckets", 16, "physical-deal EV buckets (16 or 32)")
	seed := flags.Uint64("seed", 27091402, "reproducible training seed")
	scale := flags.Float64("scale", 1, "multiplier for all entry percentages")
	percentagesJSON := flags.String("percentages", "", "optional inline JSON 6x4 Percentages array")
	if err := flags.Parse(args); err != nil {
		fatal(err)
	}
	if *iterations == 0 || (*buckets != 16 && *buckets != 32) {
		fatal(fmt.Errorf("iters must be positive and buckets must be 16 or 32"))
	}
	percentages, err := scaledPercentages(*percentagesJSON, *scale)
	if err != nil {
		fatal(err)
	}
	raw, err := os.ReadFile(*rankingPath)
	if err != nil {
		fatal(err)
	}
	ranking, err := garnet.DecodeRanking(raw)
	if err != nil {
		fatal(err)
	}
	rankingHash, err := garnet.RankingHash(ranking)
	if err != nil {
		fatal(err)
	}
	model, err := lapis.LoadSpinelModel()
	if err != nil {
		fatal(err)
	}
	referenceHash, err := lapis.EmbeddedPolicyHash()
	if err != nil {
		fatal(err)
	}
	if ranking.ReferencePolicyHash != referenceHash {
		fatal(fmt.Errorf("ranking reference policy hash %q does not match embedded Spinel policy hash %q",
			ranking.ReferencePolicyHash, referenceHash))
	}
	trainer, err := garnet.NewTrainer(garnet.TrainerConfig{
		Ranking: ranking, Percentages: percentages, BucketCount: *buckets, Seed: *seed, DrawModel: model,
	})
	if err != nil {
		fatal(err)
	}
	started := time.Now()
	if err := trainer.Train(*iterations); err != nil {
		fatal(err)
	}
	policy, err := trainer.Policy(rankingHash)
	if err != nil {
		fatal(err)
	}
	encoded, err := garnet.EncodePolicy(policy)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*out, append(encoded, '\n'), 0o644); err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "garnet train: iterations=%d infosets=%d regret_updates=%d average_updates=%d rollouts=%d elapsed=%s\n",
		policy.Iterations, len(policy.Infosets), policy.Stats.RegretUpdates, policy.Stats.AverageUpdates,
		trainer.Rollouts(), time.Since(started).Round(time.Millisecond))
}

func scaledPercentages(raw string, scale float64) (garnet.Percentages, error) {
	percentages := garnet.DefaultPercentages
	if raw != "" {
		var rows [][]float64
		if err := json.Unmarshal([]byte(raw), &rows); err != nil {
			return garnet.Percentages{}, fmt.Errorf("percentages: %w", err)
		}
		if len(rows) != int(garnet.Positions) {
			return garnet.Percentages{}, fmt.Errorf("percentages: got %d rows, want %d", len(rows), garnet.Positions)
		}
		for position, row := range rows {
			if len(row) != int(garnet.Contexts) {
				return garnet.Percentages{}, fmt.Errorf("percentages: row %d has %d values, want %d", position, len(row), garnet.Contexts)
			}
			copy(percentages[position][:], row)
		}
	}
	for position := garnet.Position(0); position < garnet.Positions; position++ {
		for context := garnet.Context(0); context < garnet.Contexts; context++ {
			percentages[position][context] *= scale
		}
	}
	if _, err := garnet.PercentagesHash(percentages); err != nil {
		return garnet.Percentages{}, err
	}
	return percentages, nil
}
