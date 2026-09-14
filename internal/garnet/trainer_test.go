package garnet

import (
	"bytes"
	"math"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/sixmaxsim"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestRegretLearningPrefersProfitableRaiseFixture(t *testing.T) {
	var regrets [2]float64
	var average [2]float64
	for range 200 {
		sigma := regretMatch(regrets)
		accumulateAverage(&average, 1, sigma, 1)
		updateRegrets(&regrets, [2]float64{-2, 3}, sigma)
	}
	if got := normalizedStrategy(average, 200); got.Raise < .99 {
		t.Fatalf("average profitable raise probability=%g, want near one", got.Raise)
	}
}

func TestActorOwnReachAverageForSixPlayers(t *testing.T) {
	low := averageFixture(t, .1)
	high := averageFixture(t, .9)
	if math.Abs(low-.25) > 1e-12 || math.Abs(high-.25) > 1e-12 {
		t.Fatalf("later UTG average mass lowHJ=%g highHJ=%g, want own reach .25", low, high)
	}
}

func averageFixture(t *testing.T, hijackRaise float64) float64 {
	t.Helper()
	percentages := Percentages{}
	for position := Position(0); position < Positions; position++ {
		for context := Context(0); context < Contexts; context++ {
			percentages[position][context] = 100
		}
	}
	trainer, err := NewTrainer(TrainerConfig{Ranking: testRanking(t, func(id handclass.ID) float64 { return float64(id) }),
		Percentages: percentages, BucketCount: 16, Seed: 31, DrawModel: cfr.Heuristic{}})
	if err != nil {
		t.Fatal(err)
	}
	game, err := sixmaxsim.New(sixmaxsim.Config{Seed: 47})
	if err != nil {
		t.Fatal(err)
	}
	entries := entryState{}
	setNext := func(regret [2]float64, action wire.Action) {
		actor, decision, _ := game.Legal()
		view := viewFromGame(game, actor, decision, entries)
		bucket, _ := trainer.bucketer.Bucket(view.Hand)
		trainer.setFor(view, bucket).regret = regret
		entries.decided[actor], entries.admitted[actor] = true, true
		if err := game.Apply(action); err != nil {
			t.Fatal(err)
		}
	}
	setNext([2]float64{1, 3}, wire.Call()) // UTG passive reach .25.
	setNext([2]float64{1 - hijackRaise, hijackRaise}, wire.Raise(200))
	for range 4 { // CO, button, small blind, and big blind call the raise.
		setNext([2]float64{1, 0}, wire.Call())
	}
	actor, decision, _ := game.Legal()
	if actor != int(UnderTheGun) {
		t.Fatalf("target actor=%d", actor)
	}
	view := viewFromGame(game, actor, decision, entries)
	bucket, _ := trainer.bucketer.Bucket(view.Hand)
	target := trainer.setFor(view, bucket)
	var reach [sixmaxsim.Seats]float64
	for seat := range reach {
		reach[seat] = 1
	}
	if err := trainer.walkAverage(mustGame(t, 47), entryState{}, reach, 1); err != nil {
		t.Fatal(err)
	}
	return target.sum[0] + target.sum[1]
}

func mustGame(t *testing.T, seed uint64) *sixmaxsim.Game {
	t.Helper()
	game, err := sixmaxsim.New(sixmaxsim.Config{Seed: seed})
	if err != nil {
		t.Fatal(err)
	}
	return game
}

func TestSerialTrainingIsReproducibleAcrossBatches(t *testing.T) {
	newTrainer := func() *Trainer {
		trainer, err := NewTrainer(TrainerConfig{Ranking: testRanking(t, func(id handclass.ID) float64 { return float64(id) }),
			Percentages: DefaultPercentages, BucketCount: 16, Seed: 59, DrawModel: cfr.Heuristic{}})
		if err != nil {
			t.Fatal(err)
		}
		return trainer
	}
	a, b := newTrainer(), newTrainer()
	if err := a.Train(2); err != nil {
		t.Fatal(err)
	}
	if err := a.Train(2); err != nil {
		t.Fatal(err)
	}
	if err := b.Train(4); err != nil {
		t.Fatal(err)
	}
	if a.Rollouts() == 0 || b.Rollouts() == 0 {
		t.Fatal("real continuation rollouts were not recorded")
	}
	rankingHash, _ := RankingHash(a.config.Ranking)
	policyA, err := a.Policy(rankingHash)
	if err != nil {
		t.Fatal(err)
	}
	policyB, err := b.Policy(rankingHash)
	if err != nil {
		t.Fatal(err)
	}
	rawA, _ := EncodePolicy(policyA)
	rawB, _ := EncodePolicy(policyB)
	if !bytes.Equal(rawA, rawB) {
		t.Fatal("same serial training seed differed across batching")
	}
}
