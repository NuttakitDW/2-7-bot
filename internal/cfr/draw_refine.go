package cfr

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/policy"
)

var handProfile = "legacy"

// Split early betting groups by made-hand value, or by retained low ranks
// and paired low cards that remove drawing outs. Keep the original group
// in each key so every refined group has exactly one legacy parent.
func refineDrawingAbstraction(a *Abstraction) {
	groups := make(map[[3]uint32]uint16)
	for id := range a.Classes {
		if handclass.Weight(handclass.ID(id)) == 0 {
			continue
		}
		info := &a.Classes[id]
		hand := handclass.Representative(handclass.ID(id))
		key := [3]uint32{uint32(info.Draw)}
		if info.Draw < 10 {
			key[1] = uint32(deuce.Eval(hand))
		} else {
			for _, rank := range policy.DrawingKeep(hand) {
				key[1] |= 1 << rank.Index()
			}
			var seen [15]bool
			for _, card := range hand {
				if card.Rank <= cards.Nine && seen[card.Rank] {
					key[2]++
				}
				seen[card.Rank] = true
			}
		}
		bucket, ok := groups[key]
		if !ok {
			if len(groups) >= 1<<16 {
				panic("drawing hand abstraction exceeds uint16")
			}
			bucket = uint16(len(groups))
			groups[key] = bucket
			a.drawParents = append(a.drawParents, info.Draw)
		}
		info.Draw = bucket
	}
	a.DrawBuckets = len(groups)
}

// RefineDrawingState transfers a legacy full-history checkpoint to the
// draw-shape profile. Draw actions and river buckets remain unchanged.
func RefineDrawingState(path string) (*Trainer, error) {
	return refineDrawingState(path, newHistoryLayout, false)
}

// RefineCompactDrawingState refines early betting hands while preserving
// the source compact layout and its chosen river abstraction.
func RefineCompactDrawingState(path string, richRiver bool) (*Trainer, error) {
	return refineDrawingState(path, newCompactLayout, richRiver)
}

func refineDrawingState(path string, layout func(*Tree, *Abstraction) *Layout, richRiver bool) (*Trainer, error) {
	makeTrainer := func(refined bool) *Trainer {
		a := buildAbstraction(richRiver)
		if refined {
			refineDrawingAbstraction(a)
		}
		tree := BuildTree()
		return NewTrainer(tree, a, layout(tree, a), nil)
	}
	old := makeTrainer(false)
	if err := old.LoadState(path); err != nil {
		return nil, err
	}
	refined := makeTrainer(true)
	refineTables(old, refined)
	return refined, nil
}
