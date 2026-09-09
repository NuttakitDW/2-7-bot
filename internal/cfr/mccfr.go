package cfr

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
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
// Workers share tables through short information-set locks. Snapshots wait
// for in-flight iterations. Multiple workers perform asynchronous updates;
// use one worker for reproducible serial training.
type Trainer struct {
	Tree   *Tree
	Abs    *Abstraction
	Layout *Layout
	Eval   deuce.Table

	// Vanilla retains signed cumulative regrets; false keeps the legacy
	// regret-matching+ update. Configure before starting workers.
	Vanilla bool
	// UniformAverage gives each sampled average update weight one. Periodic
	// discounting supplies recency weighting separately; combining it with
	// the default linear average would count recency twice.
	UniformAverage bool
	Model          Model
	ModelWeight    float64
	// ModelByHand samples a fixed opponent type once per traverser walk.
	// False retains the original decision-wise mixture. Set before Run.
	ModelByHand bool
	// AverageEvery samples the extra response-training average passes with
	// probability 1/N and multiplies their strategy-sum updates by N.
	// Zero and one retain every pass without consuming an extra random draw.
	// This preserves expected sum increments, but increases their variance;
	// visit counts remain actual sampled updates. Ignored in pure self-play.
	// Configure before Run; legacy state files do not store this setting.
	AverageEvery int
	// SampleModelDraws samples one traverser draw with an exploratory proposal
	// in fixed-model hand walks. Importance weights preserve raw regret
	// increments in expectation for frozen policies, but increase variance;
	// RM+ clipping and adaptive updates need not follow the same trajectory.
	// Average passes and self-play retain enumeration. Configure before Run.
	SampleModelDraws bool
	// UniformModelDraws uses a uniform proposal when SampleModelDraws is on.
	// With at most six candidates and three draws, inverse prefix weights
	// are bounded by 216 instead of 27000 for the policy-guided proposal.
	// This trades variance in returned utilities against regret-update variance.
	UniformModelDraws bool
	// UseDrawBaseline learns auxiliary draw values to reduce sampling variance.
	// Only sampled fixed-model walks use them. Configure before Run. Legacy
	// checkpoints omit this cache; LoadState resets it to a cold start.
	UseDrawBaseline bool
	drawBaseline    []float64
	// FixedBB, when HasFixedBB, is dealt to the big blind every hand
	// (State.DealFixed); FixedRandom deals a fresh random card instead.
	// With a fixed-card layout the button trains its group's slice, and
	// on a FixedHidden fraction of hands plays group 0 as if it did not
	// know the card. Set before Run.
	FixedBB     cards.Card
	HasFixedBB  bool
	FixedRandom bool
	FixedHidden float64
	// FixedOnly freezes base information sets, learning only appended known-card
	// groups. Initialize from a prior blueprint before Run.
	FixedOnly bool
	// FrozenRollouts samples the fully frozen suffix of an early fixed-card
	// layout instead of enumerating the traverser there. Set before Run.
	FrozenRollouts bool

	BetRegret  []float64
	BetStrat   []float64
	DrawRegret []float64
	DrawStrat  []float64
	// Visits counts average-strategy updates per set, at the set's first
	// slot, so Extract can prune sets too rarely reached to have learned
	// anything.
	BetVisits  []uint32
	DrawVisits []uint32

	mu         sync.RWMutex
	setMu      [16384]sync.Mutex
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
	tr.mu.Lock()
	if tr.UseDrawBaseline && len(tr.drawBaseline) != len(tr.DrawRegret) {
		tr.drawBaseline = make([]float64, len(tr.DrawRegret))
	}
	tr.mu.Unlock()
	model := tr.Model
	if m, ok := tr.Model.(memoizer); ok {
		model = m.WithMemo()
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			worker := &worker{tr: tr, rng: rand.New(rand.NewPCG(seed, uint64(w))), model: model}
			for {
				tr.mu.RLock()
				var iteration int64
				for {
					iteration = tr.iterations.Load()
					if iteration >= n {
						tr.mu.RUnlock()
						return
					}
					if tr.iterations.CompareAndSwap(iteration, iteration+1) {
						break
					}
				}
				worker.iterate(float64(iteration + 1))
				tr.mu.RUnlock()
			}
		}(w)
	}
	wg.Wait()
}

type worker struct {
	tr  *Trainer
	rng *rand.Rand
	// model is the fixed opponent, with a private prediction cache where
	// the model offers one.
	model Model
	state State
	// view is reused for every model query so that passing it through the
	// interface does not allocate; the model consumes it before the walk
	// continues.
	view          View
	averageOnly   bool
	fixedOpponent bool
	// Inverse probability of earlier sampled traverser draws. Zero denotes
	// the unsampled prefix (weight one), including directly constructed workers.
	drawSampleWeight float64
}

// memoizer is a Model that can serve the walkers through a prediction cache.
type memoizer interface {
	WithMemo() Model
}

func (w *worker) iterate(t float64) {
	if w.tr.UniformAverage {
		t = 1
	}
	w.deal()
	for traverser := 0; traverser < 2; traverser++ {
		w.state.Reset()
		w.drawSampleWeight = 0
		w.selectOpponent()
		w.walk(w.tr.Tree.Root, traverser, t)
	}
	if w.tr.Model != nil && w.tr.ModelWeight > 0 {
		// Average the learned policy under its own sampling distribution.
		// Model-sampled own actions cannot estimate its realization weights.
		if every := w.tr.AverageEvery; every > 1 {
			if w.rng.IntN(every) != 0 {
				return
			}
			t *= float64(every)
		}
		w.averageOnly = true
		for traverser := 0; traverser < 2; traverser++ {
			w.state.Reset()
			w.drawSampleWeight = 0
			w.walk(w.tr.Tree.Root, traverser, t)
		}
		w.averageOnly = false
	}
}

// deal starts the iteration's hand, fixing the big blind's card when the
// trainer asks for it.
func (w *worker) deal() {
	w.state.FixedGroup = 0
	switch {
	case w.tr.FixedRandom:
		card := cards.CardFromIndex(w.rng.IntN(cards.DeckSize))
		w.state.DealFixed(w.rng, card)
		if w.tr.Layout.FixedGroups > 1 && w.rng.Float64() >= w.tr.FixedHidden {
			w.state.FixedGroup = FixedGroup(card.Rank)
		}
	case w.tr.HasFixedBB:
		w.state.DealFixed(w.rng, w.tr.FixedBB)
		if w.tr.Layout.FixedGroups > 1 && w.rng.Float64() >= w.tr.FixedHidden {
			w.state.FixedGroup = FixedGroup(w.tr.FixedBB.Rank)
		}
	default:
		w.state.Deal(w.rng)
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

// frozenRollout is safe only after the last potentially trainable street.
// There are no downstream regrets or averages to update in this suffix.
func (w *worker) frozenRollout(node *Node) bool {
	return w.tr.FrozenRollouts && w.tr.FixedOnly && w.tr.Layout.early && node.Street > fixedLastStreet
}

func (w *worker) walkBet(id int32, node *Node, traverser int, t float64) float64 {
	p := int(node.Actor)
	class := Class(w.state.Hands[p][:])
	street := int(node.Street)
	slot := w.tr.Layout.BetSlotFixed(node, BetContext(p, street, &w.state.Drawn), w.tr.Abs.Bucket(street, class), w.state.FixedGroup)
	n := len(node.Acts)
	regret := w.tr.BetRegret[slot : slot+int64(n)]
	lock := w.tr.setLock(false, slot)

	if p != traverser || w.frozenRollout(node) {
		var a int
		if p != traverser && w.useModel() {
			w.view = w.state.View(w.tr.Tree, id, p, w.rng)
			action, ok := w.model.Bet(&w.view)
			a = actionIndex(node, action, ok)
		} else {
			var sigma [3]float64
			lock.Lock()
			matchRegrets(regret, sigma[:n])
			strat := w.tr.BetStrat[slot : slot+int64(n)]
			if w.recordAverage() && (!w.tr.FixedOnly || slot >= w.tr.Layout.baseBet) {
				for i := range strat {
					strat[i] += t * sigma[i]
				}
				w.tr.BetVisits[slot]++
			}
			lock.Unlock()
			a = sample(sigma[:n], w.rng)
		}
		return w.step(node, a, traverser, t)
	}

	var sigma, values [3]float64
	lock.Lock()
	matchRegrets(regret, sigma[:n])
	lock.Unlock()
	saved := w.state
	util := 0.0
	for a := 0; a < n; a++ {
		values[a] = w.step(node, a, traverser, t)
		util += sigma[a] * values[a]
		w.state = saved
	}
	if !w.averageOnly && (!w.tr.FixedOnly || slot >= w.tr.Layout.baseBet) {
		lock.Lock()
		w.updateSampledRegrets(regret, values[:n], util)
		lock.Unlock()
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
	slot := w.tr.Layout.DrawSlotFixed(street, p, AggrState(p, w.state.LastAggr),
		DrawContext(p, street, &w.state.Drawn), int(info.DrawClass), w.state.FixedGroup)
	n := int(info.NumCand)
	regret := w.tr.DrawRegret[slot : slot+int64(n)]
	lock := w.tr.setLock(true, slot)

	if p != traverser || w.frozenRollout(node) {
		if p != traverser && w.useModel() {
			w.view = w.state.View(w.tr.Tree, id, p, w.rng)
			keep, ok := w.model.Draw(&w.view)
			if !ok {
				keep = info.Keep[0]
			}
			w.state.Apply(p, street, keep)
			return w.walk(node.Next[0], traverser, t)
		}
		var sigma [MaxCand]float64
		lock.Lock()
		matchRegrets(regret, sigma[:n])
		strat := w.tr.DrawStrat[slot : slot+int64(n)]
		if w.recordAverage() && (!w.tr.FixedOnly || slot >= w.tr.Layout.baseDraw) {
			for i := range strat {
				strat[i] += t * sigma[i]
			}
			w.tr.DrawVisits[slot]++
		}
		lock.Unlock()
		w.state.Apply(p, street, info.Keep[sample(sigma[:n], w.rng)])
		return w.walk(node.Next[0], traverser, t)
	}

	var sigma, values [MaxCand]float64
	useBaseline := w.sampleModelDraws() && w.tr.UseDrawBaseline
	lock.Lock()
	matchRegrets(regret, sigma[:n])
	if useBaseline {
		copy(values[:n], w.tr.drawBaseline[slot:slot+int64(n)])
	}
	lock.Unlock()
	saved := w.state
	util := 0.0
	if w.sampleModelDraws() {
		var proposal [MaxCand]float64
		for c := 0; c < n; c++ {
			proposal[c] = .8*sigma[c] + .2/float64(n)
			if w.tr.UniformModelDraws {
				proposal[c] = 1 / float64(n)
			}
		}
		c := sample(proposal[:n], w.rng)
		prefix := w.drawSampleWeight
		weight := prefix
		if weight == 0 {
			weight = 1
		}
		w.drawSampleWeight = weight / proposal[c]
		w.state.Apply(p, street, info.Keep[c])
		child := w.walk(node.Next[0], traverser, t)
		values[c] += (child - values[c]) / proposal[c]
		for i := 0; i < n; i++ {
			util += sigma[i] * values[i]
		}
		if useBaseline {
			// Estimate from the pre-sampling snapshot, then learn the child
			// return without this node's 1/q. Use the current shared cache
			// under lock so concurrent updates are not overwritten.
			lock.Lock()
			b := &w.tr.drawBaseline[slot+int64(c)]
			*b += .1 * (child - *b)
			lock.Unlock()
		}
		w.drawSampleWeight = prefix
		w.state = saved
	} else {
		for c := 0; c < n; c++ {
			w.state.Apply(p, street, info.Keep[c])
			values[c] = w.walk(node.Next[0], traverser, t)
			util += sigma[c] * values[c]
			w.state = saved
		}
	}
	if !w.averageOnly && (!w.tr.FixedOnly || slot >= w.tr.Layout.baseDraw) {
		lock.Lock()
		w.updateSampledRegrets(regret, values[:n], util)
		lock.Unlock()
	}
	return util
}

func (w *worker) sampleModelDraws() bool {
	return w.tr.SampleModelDraws && w.tr.ModelByHand && w.fixedOpponent && !w.averageOnly
}

// Current draw action estimates already include their own 1/q. Only the
// earlier sampled prefix corrects this node's increments; never apply that
// prefix to its returned utility, which ancestors correct separately.
func (w *worker) updateSampledRegrets(regret, values []float64, utility float64) {
	if w.drawSampleWeight == 0 || w.drawSampleWeight == 1 {
		updateRegrets(regret, values, utility, w.tr.Vanilla)
		return
	}
	for i := range regret {
		regret[i] += (values[i] - utility) * w.drawSampleWeight
		if !w.tr.Vanilla {
			regret[i] = max(regret[i], 0)
		}
	}
}

func (tr *Trainer) setLock(draw bool, slot int64) *sync.Mutex {
	i := int((uint64(slot) * 11400714819323198485) >> 51)
	if draw {
		i += 8192
	}
	return &tr.setMu[i]
}

func (w *worker) useModel() bool {
	if w.tr.ModelByHand && w.tr.ModelWeight > 0 && w.tr.ModelWeight < 1 {
		return !w.averageOnly && w.fixedOpponent
	}
	return !w.averageOnly && w.tr.Model != nil && (w.tr.ModelWeight >= 1 || w.rng.Float64() < w.tr.ModelWeight)
}

func (w *worker) selectOpponent() {
	if !w.tr.ModelByHand {
		return
	}
	w.fixedOpponent = w.tr.Model != nil && w.tr.ModelWeight > 0 && (w.tr.ModelWeight >= 1 || w.rng.Float64() < w.tr.ModelWeight)
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

func updateRegrets(regret, values []float64, utility float64, vanilla bool) {
	for i := range regret {
		regret[i] = regret[i] + values[i] - utility
		if !vanilla {
			regret[i] = max(regret[i], 0)
		}
	}
}

// matchRegrets uses only the positive part of cumulative regret, and is
// uniform when no action has positive regret.
func matchRegrets(regret []float64, sigma []float64) {
	total := 0.0
	for _, r := range regret {
		total += max(r, 0)
	}
	if total <= 0 {
		for i := range sigma {
			sigma[i] = 1 / float64(len(sigma))
		}
		return
	}
	for i, r := range regret {
		sigma[i] = max(r, 0) / total
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
