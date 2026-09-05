package cfr

import (
	"math"
	"math/rand/v2"

	"github.com/nuttakit/2-7-bot/internal/deuce"
)

// Result summarises a simulated match from the first model's side.
type Result struct {
	Hands int
	// Rate is big bets per 100 hands; CI is the 95% half-width.
	Rate, CI float64
	// Counts follow model identity across seat rotations.
	Decisions, Fallbacks [2]int
}

// Simulate plays two models against each other over n decks, duplicate
// style: every deck is played twice with the seats swapped, and the pair
// is one sample. Every model decision that comes back not-ok falls to the
// fallback model, the way the runtime falls to the heuristic policy.
func Simulate(t *Tree, eval deuce.Table, a, b, fallback Model, n int, seed uint64) Result {
	counts := [2]*countedModel{{Model: a}, {Model: b}}
	a, b = counts[0], counts[1]
	rng := rand.New(rand.NewPCG(seed, 1))
	var state State
	sum, sumSq := 0.0, 0.0
	for i := 0; i < n; i++ {
		state.Deal(rng)
		first := playHand(t, eval, &state, [2]Model{a, b}, fallback, rng)
		state.Reset()
		second := playHand(t, eval, &state, [2]Model{b, a}, fallback, rng)
		net := float64(first) - float64(second)
		sum += net
		sumSq += net * net
	}
	mean := sum / float64(n)
	variance := sumSq/float64(n) - mean*mean
	// Per deck-pair chips → big bets per 100 hands, two hands per pair.
	scale := 100.0 / (2 * BigBet)
	return Result{Decisions: [2]int{counts[0].decisions, counts[1].decisions}, Fallbacks: [2]int{counts[0].fallbacks, counts[1].fallbacks}, Hands: 2 * n, Rate: mean * scale, CI: 1.96 * math.Sqrt(variance/float64(n)) * scale}
}

// playHand plays one hand out and returns the button's net chips.
func playHand(t *Tree, eval deuce.Table, s *State, seats [2]Model, fallback Model, rng *rand.Rand) int32 {
	id := t.Root
	for {
		node := &t.Nodes[id]
		switch node.Kind {
		case KindFold:
			return node.Payoff(Btn, -1)
		case KindShowdown:
			return node.Payoff(Btn, s.Winner(eval))
		case KindDraw:
			p := int(node.Actor)
			view := s.View(t, id, p, rng)
			keep, ok := seats[p].Draw(&view)
			if !ok {
				keep, _ = fallback.Draw(&view)
			}
			s.Apply(p, int(node.Street), keep)
			id = node.Next[0]
		default:
			p := int(node.Actor)
			view := s.View(t, id, p, rng)
			action, ok := seats[p].Bet(&view)
			if !ok {
				action, _ = fallback.Bet(&view)
			}
			a := actionIndex(node, action, true)
			if node.Acts[a] == Aggr {
				s.LastAggr = p
			}
			id = node.Next[a]
		}
	}
}

// countedModel observes coverage without changing actions or random draws.
type countedModel struct {
	Model
	decisions, fallbacks int
}

func (m *countedModel) Bet(v *View) (int, bool) {
	action, ok := m.Model.Bet(v)
	m.decisions++
	if !ok {
		m.fallbacks++
	}
	return action, ok
}
func (m *countedModel) Draw(v *View) (uint8, bool) {
	keep, ok := m.Model.Draw(v)
	m.decisions++
	if !ok {
		m.fallbacks++
	}
	return keep, ok
}
