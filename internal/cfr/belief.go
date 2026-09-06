package cfr

import (
	"math"
	"math/rand/v2"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
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
	if n < 1 || known.Len() > 32 {
		return nil
	}
	var available []cards.Card
	for i := 0; i < cards.DeckSize; i++ {
		c := cards.CardFromIndex(i)
		if cards.NewSet([]cards.Card{c})&known == 0 {
			available = append(available, c)
		}
	}
	particles := make([]beliefParticle, n)
	for i := range particles {
		p := &particles[i]
		copy(p.deck[:], available)
		p.end = len(available)
		rng.Shuffle(p.end, func(i, j int) { p.deck[i], p.deck[j] = p.deck[j], p.deck[i] })
		copy(p.Hand[:], p.deck[:5])
		sortHand(&p.Hand)
		p.ptr = 5
		p.Weight = 1 / float64(n)
	}
	for _, obs := range history {
		total := 0.0
		for i := range particles {
			p := &particles[i]
			v := obs.View
			v.Hand = p.Hand
			prob := 0.0
			if obs.Draw {
				if obs.Action < 0 || obs.Action > 5 || v.Street < 1 || v.Street > 3 || p.ptr+obs.Action > p.end {
					return nil
				}
				dist := m.drawProbabilities(&v)
				prob = .995*dist[obs.Action] + .005/6
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
				dist := m.betProbabilities(&v)
				prob = .995*dist[obs.Action] + .005/3
			}
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
	}
	return out
}

// BestRiverAction solves the remaining betting tree against the model.
// Hero maximizes after averaging over the hidden hands, and updates its
// belief after each hypothetical opponent response. It never chooses a
// separate hero action for each hidden hand.
func BestRiverAction(t *Tree, id int32, hero View, belief []BeliefHand, m *Empirical) (int, float64, bool) {
	if id < 0 || int(id) >= len(t.Nodes) || t.Nodes[id].Kind != KindBet || t.Nodes[id].Street != Draw3 || int(t.Nodes[id].Actor) != hero.Seat || len(belief) == 0 {
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
			p := m.betProbabilities(&v)
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
