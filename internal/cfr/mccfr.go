package cfr

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"

	"github.com/nuttakit/2-7-bot/internal/deuce"
)

// Trainer runs external-sampling MCCFR (Lanctot et al. 2009) with
// regret-matching+ and linearly weighted averaging.
//
// Each iteration deals one hand and walks it twice, once per traverser.
// At the traverser's own decisions every action is explored; the other
// seat's decisions and the cards are sampled. Regrets accumulate at the
// traverser's sets, the average strategy at the other seat's.
//
// Model, when set, replaces the other seat with a fixed opponent at a
// fraction ModelWeight of its decisions. Zero trains against self-play;
// one targets the fixed model; intermediate values target a decision-wise
// mixture. The abstraction forgets information, so these settings do not
// establish equilibrium convergence or an exploitability bound.
// Targeted runs average in separate passes that sample the learned policy.
//
// Updates and snapshots are serialized at iteration boundaries. Use one
// worker for reproducible training; extra workers do not increase throughput.
type Trainer struct {
	Tree   *Tree
	Abs    *Abstraction
	Layout *Layout
	Eval   deuce.Table

	Model       Model
	ModelWeight float64

	BetRegret  []float64
	BetStrat   []float64
	DrawRegret []float64
	DrawStrat  []float64
	// Visits counts average-strategy updates per set, at the set's first
	// slot, so Extract can prune sets too rarely reached to have learned
	// anything.
	BetVisits  []uint32
	DrawVisits []uint32

	mu         sync.Mutex
	iterations atomic.Int64
}

// NewTrainer allocates the tables.
func NewTrainer(t *Tree, a *Abstraction, l *Layout, eval deuce.Table) *Trainer {
	return &Trainer{
		Tree: t, Abs: a, Layout: l, Eval: eval,
		BetRegret:  make([]float64, l.BetSlots),
		BetStrat:   make([]float64, l.BetSlots),
		DrawRegret: make([]float64, l.DrawSlots),
		DrawStrat:  make([]float64, l.DrawSlots),
		BetVisits:  make([]uint32, l.BetSlots),
		DrawVisits: make([]uint32, l.DrawSlots),
	}
}

// Iterations reports how many hands have been walked so far.
func (tr *Trainer) Iterations() int64 { return tr.iterations.Load() }

// Run trains for n iterations across w workers.
func (tr *Trainer) Run(n int64, workers int, seed uint64) {
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			worker := &worker{tr: tr, rng: rand.New(rand.NewPCG(seed, uint64(w)))}
			for {
				tr.mu.Lock()
				if tr.iterations.Load() >= n {
					tr.mu.Unlock()
					return
				}
				worker.iterate()
				tr.mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
}

type worker struct {
	tr          *Trainer
	rng         *rand.Rand
	state       State
	averageOnly bool
}

func (w *worker) iterate() {
	t := float64(w.tr.iterations.Add(1))
	w.state.Deal(w.rng)
	for traverser := 0; traverser < 2; traverser++ {
		w.state.Reset()
		w.walk(w.tr.Tree.Root, traverser, t)
	}
	if w.tr.Model != nil && w.tr.ModelWeight > 0 {
		// Average the learned policy under its own sampling distribution.
		// Model-sampled own actions cannot estimate its realization weights.
		w.averageOnly = true
		for traverser := 0; traverser < 2; traverser++ {
			w.state.Reset()
			w.walk(w.tr.Tree.Root, traverser, t)
		}
		w.averageOnly = false
	}
}

// walk returns the traverser's expected chips from this node.
func (w *worker) walk(id int32, traverser int, t float64) float64 {
	node := &w.tr.Tree.Nodes[id]
	switch node.Kind {
	case KindFold:
		return float64(node.Payoff(traverser, -1))
	case KindShowdown:
		return float64(node.Payoff(traverser, w.state.Winner(w.tr.Eval)))
	case KindDraw:
		return w.walkDraw(id, node, traverser, t)
	default:
		return w.walkBet(id, node, traverser, t)
	}
}

func (w *worker) walkBet(id int32, node *Node, traverser int, t float64) float64 {
	p := int(node.Actor)
	class := Class(w.state.Hands[p][:])
	street := int(node.Street)
	slot := w.tr.Layout.BetSlot(node, BetContext(p, street, &w.state.Drawn), w.tr.Abs.Bucket(street, class))
	n := len(node.Acts)
	regret := w.tr.BetRegret[slot : slot+int64(n)]

	if p != traverser {
		var a int
		if w.useModel() {
			view := w.state.View(w.tr.Tree, id, p, w.rng)
			action, ok := w.tr.Model.Bet(&view)
			a = actionIndex(node, action, ok)
		} else {
			var sigma [3]float64
			matchRegrets(regret, sigma[:n])
			strat := w.tr.BetStrat[slot : slot+int64(n)]
			if w.recordAverage() {
				for i := range strat {
					strat[i] += t * sigma[i]
				}
				w.tr.BetVisits[slot]++
			}
			a = sample(sigma[:n], w.rng)
		}
		return w.step(node, a, traverser, t)
	}

	var sigma, values [3]float64
	matchRegrets(regret, sigma[:n])
	saved := w.state
	util := 0.0
	for a := 0; a < n; a++ {
		values[a] = w.step(node, a, traverser, t)
		util += sigma[a] * values[a]
		w.state = saved
	}
	for a := 0; a < n; a++ {
		if !w.averageOnly {
			regret[a] = max(regret[a]+values[a]-util, 0)
		}
	}
	return util
}

// step takes betting action a at node and continues the walk.
func (w *worker) step(node *Node, a int, traverser int, t float64) float64 {
	if node.Acts[a] == Aggr {
		w.state.LastAggr = int(node.Actor)
	}
	return w.walk(node.Next[a], traverser, t)
}

func (w *worker) walkDraw(id int32, node *Node, traverser int, t float64) float64 {
	p := int(node.Actor)
	street := int(node.Street)
	info := &w.tr.Abs.Classes[Class(w.state.Hands[p][:])]
	slot := w.tr.Layout.DrawSlot(street, p, AggrState(p, w.state.LastAggr),
		DrawContext(p, street, &w.state.Drawn), int(info.DrawClass))
	n := int(info.NumCand)
	regret := w.tr.DrawRegret[slot : slot+int64(n)]

	if p != traverser {
		if w.useModel() {
			view := w.state.View(w.tr.Tree, id, p, w.rng)
			keep, ok := w.tr.Model.Draw(&view)
			if !ok {
				keep = info.Keep[0]
			}
			w.state.Apply(p, street, keep)
			return w.walk(node.Next[0], traverser, t)
		}
		var sigma [MaxCand]float64
		matchRegrets(regret, sigma[:n])
		strat := w.tr.DrawStrat[slot : slot+int64(n)]
		if w.recordAverage() {
			for i := range strat {
				strat[i] += t * sigma[i]
			}
			w.tr.DrawVisits[slot]++
		}
		w.state.Apply(p, street, info.Keep[sample(sigma[:n], w.rng)])
		return w.walk(node.Next[0], traverser, t)
	}

	var sigma, values [MaxCand]float64
	matchRegrets(regret, sigma[:n])
	saved := w.state
	util := 0.0
	for c := 0; c < n; c++ {
		w.state.Apply(p, street, info.Keep[c])
		values[c] = w.walk(node.Next[0], traverser, t)
		util += sigma[c] * values[c]
		w.state = saved
	}
	for c := 0; c < n; c++ {
		if !w.averageOnly {
			regret[c] = max(regret[c]+values[c]-util, 0)
		}
	}
	return util
}

func (w *worker) useModel() bool {
	return !w.averageOnly && w.tr.Model != nil && (w.tr.ModelWeight >= 1 || w.rng.Float64() < w.tr.ModelWeight)
}

// actionIndex finds a model's answer among the node's legal actions,
// falling back to the passive one.
func actionIndex(node *Node, action int, ok bool) int {
	if ok {
		for i, act := range node.Acts {
			if int(act) == action {
				return i
			}
		}
	}
	for i, act := range node.Acts {
		if act == Pass {
			return i
		}
	}
	return 0
}

// matchRegrets is regret matching over non-negative regrets: proportional
// where any is positive, uniform otherwise.
func matchRegrets(regret []float64, sigma []float64) {
	total := 0.0
	for _, r := range regret {
		total += r
	}
	if total <= 0 {
		for i := range sigma {
			sigma[i] = 1 / float64(len(sigma))
		}
		return
	}
	for i, r := range regret {
		sigma[i] = r / total
	}
}

func sample(sigma []float64, rng *rand.Rand) int {
	u := rng.Float64()
	for i, p := range sigma {
		u -= p
		if u < 0 {
			return i
		}
	}
	return len(sigma) - 1
}

func (w *worker) recordAverage() bool {
	return w.averageOnly || w.tr.Model == nil || w.tr.ModelWeight <= 0
}
