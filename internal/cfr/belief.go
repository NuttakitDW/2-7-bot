package cfr

import (
	"math"
	"math/rand/v2"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/handclass"
)

// OpponentObservation contains public evidence at the time of a decision.
// View.Hand is ignored: the filter supplies each hypothetical private hand.
type OpponentObservation struct {
	View   View
	Draw   bool
	Action int // betting action, or observed discard count
}

type BeliefHand struct {
	Hand   [5]cards.Card
	Weight float64
	// Dead contains hypothetical prior opponent discards, for future draws.
	Dead cards.Set
}

type beliefParticle struct {
	BeliefHand
	deck     [cards.DeckSize]cards.Card
	ptr, end int
}

func (m *Empirical) betProbabilities(v *View) [6]float64 {
	p := m.predictBet(v)
	if !v.Facing {
		p[Fold] = 0
	}
	if !v.CanRaise {
		p[Aggr] = 0
	}
	s := p[0] + p[1] + p[2]
	if s == 0 {
		return [6]float64{0, 1}
	}
	for i := 0; i < 3; i++ {
		p[i] /= s
	}
	return p
}

// responseProbabilities predicts the opponent's next betting action for a
// river solve. ResponseAlpha above 1 sharpens the fit toward its mode, the
// reading of a purified opponent; likelihoods for belief stay unsharpened.
func (m *Empirical) responseProbabilities(v *View) [6]float64 {
	p := m.betProbabilities(v)
	if m.ResponseAlpha <= 1 {
		return p
	}
	p = sharpen(p, 3, m.ResponseAlpha)
	s := p[0] + p[1] + p[2]
	if s == 0 {
		return [6]float64{0, 1}
	}
	for i := 0; i < 3; i++ {
		p[i] /= s
	}
	return p
}

func (m *Empirical) drawProbabilities(v *View) [6]float64 {
	p := m.predictDraw(v)
	s := 0.0
	for _, x := range p {
		s += x
	}
	for i := range p {
		p[i] /= s
	}
	return p
}

// OpponentBelief uses sequential importance sampling and resampling. All of
// hero's known cards, including discards, are excluded throughout. The model
// conditions on public betting actions and draw counts; no actual opponent
// private cards are inputs. Small likelihood floors tolerate model error.
func OpponentBelief(m *Empirical, history []OpponentObservation, known cards.Set, rng *rand.Rand, n int) []BeliefHand {
	return OpponentBeliefHolding(m, history, known, 0, rng, n)
}

// OpponentBeliefHolding is OpponentBelief with cards the opponent's initial
// hand is known to contain (the arena's constant big-blind card), which
// every particle starts with.
func OpponentBeliefHolding(m *Empirical, history []OpponentObservation, known, held cards.Set, rng *rand.Rand, n int) []BeliefHand {
	if n < 1 || known.Len() > 32 || held.Len() > 5 || known&held != 0 {
		return nil
	}
	var available, fixed []cards.Card
	for i := 0; i < cards.DeckSize; i++ {
		c := cards.CardFromIndex(i)
		set := cards.NewSet([]cards.Card{c})
		switch {
		case set&held != 0:
			fixed = append(fixed, c)
		case set&known == 0:
			available = append(available, c)
		}
	}
	particles := make([]beliefParticle, n)
	for i := range particles {
		p := &particles[i]
		// The deck starts with the held cards, then the shuffled rest: the
		// first five are the initial hand, and replacements follow.
		copy(p.deck[:], fixed)
		copy(p.deck[len(fixed):], available)
		p.end = len(fixed) + len(available)
		rng.Shuffle(len(available), func(i, j int) {
			i, j = i+len(fixed), j+len(fixed)
			p.deck[i], p.deck[j] = p.deck[j], p.deck[i]
		})
		copy(p.Hand[:], p.deck[:5])
		sortHand(&p.Hand)
		p.ptr = 5
		p.Weight = 1 / float64(n)
	}
	for _, obs := range history {
		total := 0.0
		// Resampled particles often share private features. The public view is
		// fixed for this observation, so their model likelihoods are identical.
		likelihood := make(map[handclass.ID]float64, len(particles))
		for i := range particles {
			p := &particles[i]
			v := obs.View
			v.Hand = p.Hand
			class := handclass.Of(p.Hand[:])
			prob, cached := likelihood[class]
			if obs.Draw {
				if obs.Action < 0 || obs.Action > 5 || v.Street < 1 || v.Street > 3 || p.ptr+obs.Action > p.end {
					return nil
				}
				if !cached {
					dist := m.drawProbabilities(&v)
					prob = .995*dist[obs.Action] + .005/6
				}
				keep := m.keepForCount(&v, obs.Action)
				for k := range p.Hand {
					if keep&(1<<k) == 0 {
						p.Hand[k] = p.deck[p.ptr]
						p.ptr++
					}
				}
				sortHand(&p.Hand)
			} else {
				if obs.Action < 0 || obs.Action > 2 || v.Street < 0 || v.Street > 3 {
					return nil
				}
				if !cached {
					dist := m.betProbabilities(&v)
					prob = .995*dist[obs.Action] + .005/3
				}
			}
			likelihood[class] = prob
			p.Weight *= prob
			total += p.Weight
		}
		if total <= 0 {
			return nil
		}
		square := 0.0
		for i := range particles {
			particles[i].Weight /= total
			square += particles[i].Weight * particles[i].Weight
		}
		if 1/square < float64(n)*.65 {
			// Systematic resampling. Re-randomize the unobserved deck suffix
			// so clones can receive different future replacement cards.
			next := make([]beliefParticle, n)
			j, cumulative := 0, particles[0].Weight
			u := rng.Float64() / float64(n)
			for i := range next {
				at := u + float64(i)/float64(n)
				for at > cumulative && j < n-1 {
					j++
					cumulative += particles[j].Weight
				}
				next[i] = particles[j]
				p := &next[i]
				p.Weight = 1 / float64(n)
				rng.Shuffle(p.end-p.ptr, func(a, b int) { a += p.ptr; b += p.ptr; p.deck[a], p.deck[b] = p.deck[b], p.deck[a] })
			}
			particles = next
		}
	}
	out := make([]BeliefHand, n)
	for i, p := range particles {
		out[i] = p.BeliefHand
		out[i].Dead = cards.NewSet(p.deck[:p.ptr]) &^ cards.NewSet(p.Hand[:])
	}
	return out
}

// BestRiverAction solves the remaining betting tree against the model.
// Hero maximizes after averaging over the hidden hands, and updates its
// belief after each hypothetical opponent response. It never chooses a
// separate hero action for each hidden hand.
func BestRiverAction(t *Tree, id int32, hero View, belief []BeliefHand, m *Empirical) (int, float64, bool) {
	return solveRiver(t, id, hero, belief, m, true, nil)
}

// RiverContinuationValue also accepts a river node where the opponent acts
// first. Future hero decisions still maximize only after averaging hidden hands.
func RiverContinuationValue(t *Tree, id int32, hero View, belief []BeliefHand, m *Empirical) (float64, bool) {
	_, ev, ok := solveRiver(t, id, hero, belief, m, false, nil)
	return ev, ok
}

type riverPolicyKey struct {
	node int32
	hand handclass.ID
}

// A cache is local to a single root/public draw context. Opponent policy
// features don't depend on hero's replacement card, so those probabilities
// can be reused across that root's alternative private hero outcomes.
func solveRiver(t *Tree, id int32, hero View, belief []BeliefHand, m *Empirical, requireHero bool, cache map[riverPolicyKey][6]float64) (int, float64, bool) {
	if t == nil || m == nil || hero.Seat < 0 || hero.Seat > 1 || id < 0 || int(id) >= len(t.Nodes) || t.Nodes[id].Kind != KindBet || t.Nodes[id].Street != Draw3 || (requireHero && int(t.Nodes[id].Actor) != hero.Seat) || len(belief) == 0 {
		return 0, 0, false
	}
	winners := make([]int, len(belief))
	weights := make([]float64, len(belief))
	value := deuce.Eval(hero.Hand[:])
	total := 0.0
	for i, p := range belief {
		opponent := deuce.Eval(p.Hand[:])
		winners[i] = -1
		if value > opponent {
			winners[i] = hero.Seat
		} else if value < opponent {
			winners[i] = 1 - hero.Seat
		}
		weights[i] = p.Weight
		total += p.Weight
	}
	if total <= 0 {
		return 0, 0, false
	}
	var solve func(int32, int, []float64) (int, float64)
	solve = func(nodeID int32, lastAggr int, weight []float64) (int, float64) {
		n := &t.Nodes[nodeID]
		if n.Kind == KindFold || n.Kind == KindShowdown {
			ev := 0.0
			for i, w := range weight {
				ev += w * float64(n.Payoff(hero.Seat, winners[i]))
			}
			return Pass, ev
		}
		if int(n.Actor) == hero.Seat {
			best, ev := Pass, math.Inf(-1)
			for a, action := range n.Acts {
				last := lastAggr
				if action == Aggr {
					last = hero.Seat
				}
				_, v := solve(n.Next[a], last, weight)
				if v > ev+1e-9 {
					best, ev = int(action), v
				}
			}
			return best, ev
		}
		branches := make([][]float64, len(n.Acts))
		for a := range branches {
			branches[a] = make([]float64, len(weight))
		}
		v := hero
		v.Seat = 1 - hero.Seat
		v.Node = nodeID
		v.Facing = n.Facing
		v.CanRaise = n.Acts[len(n.Acts)-1] == Aggr
		v.Wagers = int(n.Wagers)
		v.Pot = n.Commit[0] + n.Commit[1]
		v.ToCall = max(0, n.Commit[1-v.Seat]-n.Commit[v.Seat])
		v.LastAggr = lastAggr
		for i, w := range weight {
			if w == 0 {
				continue
			}
			v.Hand = belief[i].Hand
			var p [6]float64
			if cache == nil {
				p = m.responseProbabilities(&v)
			} else {
				key := riverPolicyKey{nodeID, handclass.Of(v.Hand[:])}
				var found bool
				p, found = cache[key]
				if !found {
					p = m.responseProbabilities(&v)
					cache[key] = p
				}
			}
			for a, action := range n.Acts {
				branches[a][i] = w * p[action]
			}
		}
		ev := 0.0
		for a, action := range n.Acts {
			last := lastAggr
			if action == Aggr {
				last = 1 - hero.Seat
			}
			_, value := solve(n.Next[a], last, branches[a])
			ev += value
		}
		return Pass, ev
	}
	a, ev := solve(id, hero.LastAggr, weights)
	return a, ev / total, true
}
