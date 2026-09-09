package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/deuce"
)

func trainRiver(args []string) error {
	fs := flag.NewFlagSet("river-train", flag.ContinueOnError)
	model := fs.String("model", "", "empirical JSON policy to retain before the river and target as opponent")
	out := fs.String("out", "", "self-contained response JSON output")
	iters := fs.Int64("iters", 1_000_000, "sampled deals")
	workers := fs.Int("workers", 1, "concurrent workers; 1 is reproducible")
	seed := fs.Uint64("seed", 1, "training seed")
	abstraction := fs.String("abstraction", "rank", "river hands: rank, class, or legacy")
	minVisits := fs.Uint64("minvisits", 50, "minimum sampled visits to export a river state")
	every := fs.Duration("every", 2*time.Minute, "snapshot interval")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *model == "" || *out == "" || *iters < 1 || *workers < 1 || *workers > 128 || *every <= 0 || *minVisits == 0 {
		return fmt.Errorf("model, out, positive iters, workers (1..128), minvisits and every are required")
	}
	if *abstraction != "rank" && *abstraction != "class" && *abstraction != "legacy" {
		return fmt.Errorf("unknown river abstraction %q", *abstraction)
	}
	if err := distinctRefinePaths(*model, *out); err != nil {
		return err
	}
	raw, err := os.ReadFile(*model)
	if err != nil {
		return err
	}
	m, err := cfr.DecodeEmpirical(raw)
	if err != nil {
		return err
	}
	m, err = m.WithMemoBits(23)
	if err != nil {
		return err
	}
	tr := cfr.NewRiverTrainer(cfr.BuildTree(), m.Mode(), m.Mode(), deuce.NewTable(), *abstraction)
	start := time.Now()
	save := func() error {
		policy := tr.Extract(*minVisits)
		f, err := os.Create(*out + ".tmp")
		if err != nil {
			return err
		}
		if err := cfr.EncodeRiverResponse(raw, policy, f); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		if err := os.Rename(*out+".tmp", *out); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "%s: %d deals, %.0f/s, %d river sets\n", time.Since(start).Round(time.Second), policy.Iterations, float64(policy.Iterations)/time.Since(start).Seconds(), len(policy.Rows))
		return nil
	}
	done := make(chan struct{})
	go func() { tr.Run(*iters, *workers, *seed); close(done) }()
	ticker := time.NewTicker(*every)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return save()
		case <-ticker.C:
			if err := save(); err != nil {
				fmt.Fprintf(os.Stderr, "river checkpoint: %v\n", err)
			}
		}
	}
}
