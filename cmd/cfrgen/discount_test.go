package main

import (
	"math"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cfr"
)

func TestRunDiscountedStopsAtTargetAndScalesCompletedPeriods(t *testing.T) {
	tr := cfr.NewTrainer(&cfr.Tree{Nodes: []cfr.Node{{Kind: cfr.KindFold}}}, &cfr.Abstraction{}, &cfr.Layout{}, nil)
	tr.BetRegret = []float64{12, -6}
	tr.DrawStrat = []float64{9}
	runDiscounted(tr, 25, 10, 2, 7)
	if tr.Iterations() != 25 {
		t.Fatalf("iterations %d, want 25", tr.Iterations())
	}
	if math.Abs(tr.BetRegret[0]-4) > 1e-12 || math.Abs(tr.BetRegret[1]+2) > 1e-12 || math.Abs(tr.DrawStrat[0]-3) > 1e-12 {
		t.Fatalf("wrong completed-period discount: %v %v", tr.BetRegret, tr.DrawStrat)
	}
}
