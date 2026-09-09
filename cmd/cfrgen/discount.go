package main

import "github.com/nuttakit/2-7-bot/internal/cfr"

// This is the periodic MCCFR discount experiment from Brown and Sandholm,
// https://arxiv.org/abs/1809.04040, using iteration periods rather than their
// node-touch periods. Signed regrets and uniformly weighted sample updates
// are configured by train. It is not the full-tree DCFR algorithm.
func runDiscounted(tr *cfr.Trainer, target, every int64, workers int, seed uint64) {
	for tr.Iterations() < target {
		start := tr.Iterations()
		period := start/every + 1
		end := start + min(every, target-start)
		// Run creates worker RNGs. Distinct period seeds avoid replaying the
		// same deals on each call. Multiple workers remain asynchronous.
		tr.Run(end, workers, seed+uint64(period-1))
		if end%every == 0 {
			// This factor is always in (0,1], so validation cannot fail.
			_ = tr.Discount(float64(period) / (float64(period) + 1))
		}
	}
}
