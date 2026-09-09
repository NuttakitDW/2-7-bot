package cfr

import (
	"math/rand/v2"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/nuttakit/2-7-bot/internal/deuce"
)

type riverSet struct {
	regret, sum [3]float64
	visits      uint64
}

type riverShard struct {
	sync.Mutex
	sets map[uint64]*riverSet
}

// RiverTrainer fixes the pre-river policy, then applies external-sampling
// CFR to river betting against a fixed opponent. Its sparse information sets
// aggregate hidden deals before choosing an action; there is no per-deal
// clairvoyant maximization. Concurrent workers update whole sets under locks.
type RiverTrainer struct {
	Tree           *Tree
	Base, Opponent Model
	Eval           deuce.Table
	Abstraction    string
	contexts       []uint32
	shards         [256]riverShard
	iterations     atomic.Int64
	mu             sync.RWMutex
}

func NewRiverTrainer(tree *Tree, base, opponent Model, eval deuce.Table, abstraction string) *RiverTrainer {
	tr := &RiverTrainer{Tree: tree, Base: base, Opponent: opponent, Eval: eval, Abstraction: abstraction, contexts: newRiverContexts(tree)}
	for i := range tr.shards {
		tr.shards[i].sets = make(map[uint64]*riverSet)
	}
	return tr
}

func (tr *RiverTrainer) Iterations() int64 { return tr.iterations.Load() }

// Run adds samples until the total target. A single worker and seed are
// reproducible; asynchronous multi-worker training has ordering variation.
func (tr *RiverTrainer) Run(target int64, workers int, seed uint64) {
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			w := riverWorker{tr: tr, rng: rand.New(rand.NewPCG(seed, uint64(worker)))}
			for {
				tr.mu.RLock()
				i := tr.iterations.Load()
				for i < target && !tr.iterations.CompareAndSwap(i, i+1) {
					i = tr.iterations.Load()
				}
				if i >= target {
					tr.mu.RUnlock()
					return
				}
				w.state.Deal(w.rng)
				for hero := 0; hero < 2; hero++ {
					w.state.Reset()
					if root, ok := w.prefix(hero); ok {
						w.walk(root, hero, w.state.Winner(tr.Eval), 1, float64(i+1))
					}
				}
				tr.mu.RUnlock()
			}
		}(worker)
	}
	wg.Wait()
}

// Extract snapshots the average policy. The visit floor counts sampled
// hidden deals at a state, including counterfactual own-action branches.
func (tr *RiverTrainer) Extract(minVisits uint64) *RiverPolicy {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	p := &RiverPolicy{Version: 1, Abstraction: tr.Abstraction, Iterations: tr.Iterations()}
	for i := range tr.shards {
		shard := &tr.shards[i]
		shard.Lock()
		for key, set := range shard.sets {
			if set.visits < minVisits {
				continue
			}
			var row RiverRow
			row.Key, row.Visits = key, set.visits
			quantise(set.sum[:], row.P[:])
			if row.P != [3]uint8{} {
				p.Rows = append(p.Rows, row)
			}
		}
		shard.Unlock()
	}
	sort.Slice(p.Rows, func(i, j int) bool { return p.Rows[i].Key < p.Rows[j].Key })
	return p
}

type riverWorker struct {
	tr    *RiverTrainer
	rng   *rand.Rand
	state State
	view  View
}

func (w *riverWorker) prefix(hero int) (int32, bool) {
	id := w.tr.Tree.Root
	for {
		n := &w.tr.Tree.Nodes[id]
		if n.Kind == KindFold || n.Kind == KindShowdown {
			return id, false
		}
		if n.Kind == KindBet && n.Street == Draw3 {
			return id, true
		}
		p := int(n.Actor)
		model := w.tr.Opponent
		if p == hero {
			model = w.tr.Base
		}
		w.view = w.state.View(w.tr.Tree, id, p, w.rng)
		if n.Kind == KindDraw {
			keep, ok := model.Draw(&w.view)
			if !ok {
				keep, _ = (Heuristic{}).Draw(&w.view)
			}
			w.state.Apply(p, int(n.Street), keep)
			id = n.Next[0]
		} else {
			action, ok := model.Bet(&w.view)
			a := actionIndex(n, action, ok)
			if n.Acts[a] == Aggr {
				w.state.LastAggr = p
			}
			id = n.Next[a]
		}
	}
}

func (w *riverWorker) walk(id int32, hero, winner int, reach, iteration float64) float64 {
	n := &w.tr.Tree.Nodes[id]
	if n.Kind == KindFold || n.Kind == KindShowdown {
		return float64(n.Payoff(hero, winner))
	}
	p := int(n.Actor)
	w.view = w.state.View(w.tr.Tree, id, p, w.rng)
	if p != hero {
		action, ok := w.tr.Opponent.Bet(&w.view)
		a := actionIndex(n, action, ok)
		return w.step(n, a, hero, winner, reach, iteration)
	}
	key := riverKey(&w.view, w.tr.contexts, w.tr.Abstraction)
	shard := &w.tr.shards[(key*0x9e3779b97f4a7c15)>>56]
	shard.Lock()
	set := shard.sets[key]
	if set == nil {
		set = &riverSet{}
		shard.sets[key] = set
	}
	var regret, sigma [3]float64
	for i, action := range n.Acts {
		regret[i] = set.regret[action]
	}
	matchRegrets(regret[:len(n.Acts)], sigma[:len(n.Acts)])
	for i, action := range n.Acts {
		set.sum[action] += reach * iteration * sigma[i]
	}
	set.visits++
	shard.Unlock()
	var values [3]float64
	utility := 0.0
	for i := range n.Acts {
		values[i] = w.step(n, i, hero, winner, reach*sigma[i], iteration)
		utility += sigma[i] * values[i]
	}
	shard.Lock()
	for i, action := range n.Acts {
		set.regret[action] = max(0, set.regret[action]+values[i]-utility)
	}
	shard.Unlock()
	return utility
}

func (w *riverWorker) step(n *Node, a, hero, winner int, reach, iteration float64) float64 {
	previous := w.state.LastAggr
	if n.Acts[a] == Aggr {
		w.state.LastAggr = int(n.Actor)
	}
	value := w.walk(n.Next[a], hero, winner, reach, iteration)
	w.state.LastAggr = previous
	return value
}
