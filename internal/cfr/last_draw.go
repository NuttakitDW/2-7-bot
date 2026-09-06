package cfr

import (
	"math"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/handclass"
)

// OneCardRiverValue compares pat (-1) with replacing a sorted-hand index.
// It is scoped to the button's final draw, after the opponent has already
// drawn. Each replacement conditions the hidden-hand distribution before any
// future hero action is optimized, avoiding decisions based on hidden cards.
func OneCardRiverValue(tree *Tree, id int32, hero View, belief []BeliefHand, known cards.Set, discard int, model *Empirical) (float64, bool) {
	if tree == nil || model == nil || id < 0 || int(id) >= len(tree.Nodes) || hero.Seat != Btn || discard < -1 || discard >= 5 || hero.Drawn[BB][Draw3] < 0 || hero.Drawn[BB][Draw3] > 5 {
		return 0, false
	}
	draw := &tree.Nodes[id]
	if draw.Kind != KindDraw || draw.Street != Draw3 || draw.Actor != Btn {
		return 0, false
	}
	root := draw.Next[0]
	node := &tree.Nodes[root]
	known |= cards.NewSet(hero.Hand[:])
	valid := make([]BeliefHand, 0, len(belief))
	total := 0.0
	for _, p := range belief {
		held := cards.NewSet(p.Hand[:])
		if held.Len() != 5 || held&known != 0 || p.Dead&(known|held) != 0 || p.Weight <= 0 || math.IsNaN(p.Weight) || math.IsInf(p.Weight, 0) {
			continue
		}
		valid = append(valid, p)
		total += p.Weight
	}
	if total <= 0 || math.IsInf(total, 0) {
		return 0, false
	}
	hero.Node, hero.Street, hero.Wagers, hero.Facing, hero.CanRaise = root, Draw3, 0, false, true
	hero.Pot, hero.ToCall = node.Commit[0]+node.Commit[1], 0
	hero.Drawn[Btn][Draw3] = 0
	if discard < 0 {
		return RiverContinuationValue(tree, root, hero, valid, model)
	}
	hero.Drawn[Btn][Draw3] = 1
	ev, mass := 0.0, 0.0
	cache := make(map[riverPolicyKey][6]float64)
	for index := 0; index < cards.DeckSize; index++ {
		replacement := cards.CardFromIndex(index)
		cardSet := cards.NewSet([]cards.Card{replacement})
		if known&cardSet != 0 {
			continue
		}
		next := hero
		next.Hand[discard] = replacement
		conditional := make([]BeliefHand, 0, len(valid))
		positions := map[handclass.ID]int{}
		probability := 0.0
		for _, p := range valid {
			blocked := known | p.Dead | cards.NewSet(p.Hand[:])
			if blocked&cardSet != 0 {
				continue
			}
			remaining := cards.DeckSize - blocked.Len()
			if remaining <= 0 {
				continue
			}
			weight := p.Weight / float64(remaining)
			probability += weight
			// With no further draws, rank multiset and flushness are sufficient for
			// this fitted opponent policy and showdown. Dead cards no longer matter.
			class := handclass.Of(p.Hand[:])
			if at, ok := positions[class]; ok {
				conditional[at].Weight += weight
			} else {
				positions[class] = len(conditional)
				conditional = append(conditional, BeliefHand{Hand: p.Hand, Weight: weight})
			}
		}
		if probability <= 0 {
			continue
		}
		_, value, ok := solveRiver(tree, root, next, conditional, model, false, cache)
		if !ok {
			return 0, false
		}
		ev += probability * value / total
		mass += probability / total
	}
	if math.Abs(mass-1) > 1e-8 {
		return 0, false
	}
	return ev, true
}
