package cfr

import (
	"math/rand/v2"
	"testing"
)

func TestVanillaRegretRetainsNegativeHistory(t *testing.T) {
	regret := []float64{-5, 5}
	updateRegrets(regret, []float64{3, 0}, 0, true)
	if regret[0] != -2 || regret[1] != 5 {
		t.Fatalf("lost signed history: %v", regret)
	}
	sigma := make([]float64, 2)
	matchRegrets(regret, sigma)
	if sigma[0] != 0 || sigma[1] != 1 {
		t.Fatalf("negative regret received probability: %v", sigma)
	}
	updateRegrets(regret, []float64{3, 0}, 0, false)
	if regret[0] != 1 || regret[1] != 5 {
		t.Fatal("legacy update changed")
	}
	matchRegrets([]float64{-4, -2}, sigma)
	if sigma[0] != .5 || sigma[1] != .5 {
		t.Fatal("all-negative regrets must use uniform policy")
	}
}

func TestVanillaLearnsBetterActionWithNoisySamples(t *testing.T) {
	rng := rand.New(rand.NewPCG(313, 1))
	regret, sigma := make([]float64, 2), make([]float64, 2)
	average := 0.0
	for i := 0; i < 100000; i++ {
		matchRegrets(regret, sigma)
		average += sigma[0]
		values := []float64{1 + rng.NormFloat64()*10, rng.NormFloat64() * 10}
		updateRegrets(regret, values, sigma[0]*values[0]+sigma[1]*values[1], true)
	}
	if average/100000 < .95 {
		t.Fatalf("better action learned frequency %.3f", average/100000)
	}
}
