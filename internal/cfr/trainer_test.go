package cfr

import (
	"math/rand/v2"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/handclass"
)

func TestTargetedTrainingExportsStrategy(t *testing.T) {
	tree := BuildTree()
	abs := BuildAbstraction()
	layout := NewLayout(tree, abs)
	tr := NewTrainer(tree, abs, layout, deuce.NewTable())
	tr.Model, tr.ModelWeight = Heuristic{Cobalt: true}, 1
	tr.Run(40, 1, 123)
	bp := tr.Extract(1)
	for name, table := range map[string][]uint8{"bet": bp.Bet, "draw": bp.Draw} {
		found := false
		for _, p := range table {
			if p != 0 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("targeted training exported no %s strategy", name)
		}
	}
}

// A one-decision game with known values: passing loses 50, aggression
// wins 100. The fixed model always passes, so averaging its actions would
// export the wrong answer even though the learner finds the best action.
func TestTargetedTrainerLearnsKnownBestResponse(t *testing.T) {
	tree := &Tree{Nodes: []Node{
		{Kind: KindBet, Actor: Btn, Acts: []uint8{Pass, Aggr}, Next: [3]int32{1, 2}},
		{Kind: KindFold, Actor: Btn, Commit: [2]int32{50, 100}},
		{Kind: KindFold, Actor: BB, Commit: [2]int32{50, 100}},
	}}
	abs := BuildAbstraction()
	layout := NewLayout(tree, abs)
	tr := NewTrainer(tree, abs, layout, nil)
	tr.Model, tr.ModelWeight = passiveModel{}, 1
	w := worker{tr: tr, rng: rand.New(rand.NewPCG(17, 1))}
	w.state.Deal(w.rng)
	for i := 1; i <= 100; i++ {
		w.walk(0, Btn, float64(i))
		before := append([]float64(nil), tr.BetRegret...)
		w.averageOnly = true
		w.walk(0, BB, float64(i))
		if !slices.Equal(before, tr.BetRegret) {
			t.Fatal("averaging changed regrets")
		}
		w.averageOnly = false
	}
	pl := Player{Tree: tree, Abs: abs, Layout: layout, BP: tr.Extract(1)}
	v := w.state.View(tree, 0, Btn, w.rng)
	probs := pl.Probabilities(&v)
	if probs[1] < .99 {
		t.Fatalf("best action probability = %v, want >= .99", probs)
	}
}

type passiveModel struct{}

func (passiveModel) Bet(*View) (int, bool)    { return Pass, true }
func (passiveModel) Draw(*View) (uint8, bool) { return 31, true }

func TestConcurrentIterationsStopAtRequestedCount(t *testing.T) {
	tree := &Tree{Nodes: []Node{{Kind: KindFold, Actor: Btn}}}
	tr := NewTrainer(tree, &Abstraction{}, &Layout{}, nil)
	tr.Run(17, 4, 9)
	if got := tr.Iterations(); got != 17 {
		t.Fatalf("ran %d iterations, want 17", got)
	}
	tr.Run(20, 2, 9)
	if got := tr.Iterations(); got != 20 {
		t.Fatalf("resume ran %d iterations, want 20", got)
	}
}

type overlappingModel struct {
	calls          atomic.Int32
	ready, release chan struct{}
}

func (m *overlappingModel) Bet(*View) (int, bool) {
	if m.calls.Add(1) == 2 {
		close(m.ready)
	}
	<-m.release
	return Pass, true
}
func (m *overlappingModel) Draw(*View) (uint8, bool) { return 31, true }

func TestTrainingWorkersCanWalkConcurrently(t *testing.T) {
	tree := &Tree{Nodes: []Node{{Kind: KindBet, Actor: Btn, Acts: []uint8{Pass}, Next: [3]int32{1}}, {Kind: KindFold, Actor: BB}}}
	abs := BuildAbstraction()
	layout := NewLayout(tree, abs)
	layout.DrawSlots = 0 // This synthetic tree has no draw nodes.
	tr := NewTrainer(tree, abs, layout, nil)
	m := &overlappingModel{ready: make(chan struct{}), release: make(chan struct{})}
	tr.Model, tr.ModelWeight = m, 1
	done := make(chan struct{})
	go func() { tr.Run(2, 2, 99); close(done) }()
	select {
	case <-m.ready:
	case <-time.After(2 * time.Second):
		t.Error("training walkers remained serialized")
	}
	saved := make(chan error, 1)
	attempted := make(chan struct{})
	path := filepath.Join(t.TempDir(), "blocked-state.bin")
	go func() { close(attempted); saved <- tr.SaveState(path) }()
	<-attempted
	select {
	case err := <-saved:
		t.Errorf("snapshot completed with blocked iterations: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(m.release)
	<-done
	select {
	case err := <-saved:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("snapshot did not finish after iterations completed")
	}
	if tr.Iterations() != 2 {
		t.Fatalf("iteration count=%d", tr.Iterations())
	}
}

func TestParallelTrainingPreservesVisitsAndSnapshotBoundary(t *testing.T) {
	tree := &Tree{Nodes: []Node{
		{Kind: KindBet, Actor: Btn, Acts: []uint8{Pass, Aggr}, Next: [3]int32{1, 2}},
		{Kind: KindFold, Actor: Btn, Commit: [2]int32{50, 100}},
		{Kind: KindFold, Actor: BB, Commit: [2]int32{50, 100}},
	}}
	abs := BuildAbstraction()
	layout := &Layout{BetSlots: handclass.Num * 2}
	tr := NewTrainer(tree, abs, layout, nil)
	done := make(chan struct{})
	go func() { tr.Run(20000, 4, 28); close(done) }()
	path := filepath.Join(t.TempDir(), "state.bin")
	if err := tr.SaveState(path); err != nil {
		t.Fatal(err)
	}
	snapshot := NewTrainer(tree, abs, layout, nil)
	if err := snapshot.LoadState(path); err != nil {
		t.Fatal(err)
	}
	visits := uint64(0)
	for _, v := range snapshot.BetVisits {
		visits += uint64(v)
	}
	if visits != uint64(snapshot.Iterations()) {
		t.Fatalf("in-flight iteration entered snapshot: visits=%d iterations=%d", visits, snapshot.Iterations())
	}
	<-done
	visits = 0
	for _, v := range tr.BetVisits {
		visits += uint64(v)
	}
	if visits != 20000 || tr.Iterations() != 20000 {
		t.Fatalf("lost updates: visits=%d iterations=%d", visits, tr.Iterations())
	}
}
