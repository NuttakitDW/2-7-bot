package cfr

// RefineCompactState initializes the compact-rich layout from a compact
// checkpoint. This transfers a prior, not newly learned river decisions.
// Continue training with the compact-rich build profile and a fresh seed.
func RefineCompactState(path string) (*Trainer, error) {
	return refineState(path, newCompactLayout)
}

// RefineHistoryState preserves the complete public betting history while
// refining river hands. Continue with the history-rich build profile.
func RefineHistoryState(path string) (*Trainer, error) {
	return refineState(path, newHistoryLayout)
}

func refineState(path string, layout func(*Tree, *Abstraction) *Layout) (*Trainer, error) {
	makeTrainer := func(rich bool) *Trainer {
		tree, abs := BuildTree(), buildAbstraction(rich)
		return NewTrainer(tree, abs, layout(tree, abs), nil)
	}
	old := makeTrainer(false)
	if err := old.LoadState(path); err != nil {
		return nil, err
	}
	refined := makeTrainer(true)
	refineTables(old, refined)
	return refined, nil
}

// Both trees must be constructed identically, with the destination refining
// early drawing hands or river hands. Unchanged buckets copy directly.
// The destination is new
// and has not been exposed to workers.
func refineTables(old, refined *Trainer) {
	old.mu.Lock()
	defer old.mu.Unlock()
	copy(refined.DrawRegret, old.DrawRegret)
	copy(refined.DrawStrat, old.DrawStrat)
	copy(refined.DrawVisits, old.DrawVisits)
	refined.iterations.Store(old.Iterations())
	var children [NumFinalBuckets]int
	for _, parent := range refined.Abs.finalParents {
		children[parent]++
	}
	var drawChildren [NumDrawBuckets]int
	for _, parent := range refined.Abs.drawParents {
		drawChildren[parent]++
	}
	seen := make(map[int64]bool)
	for i := range refined.Tree.Nodes {
		node := &refined.Tree.Nodes[i]
		if node.Kind != KindBet || seen[node.Offset] {
			continue
		}
		seen[node.Offset] = true
		street, n := int(node.Street), int64(len(node.Acts))
		for ctx := 0; ctx < BetContexts(street); ctx++ {
			for bucket := 0; bucket < refined.Layout.Buckets(street); bucket++ {
				parent, split := bucket, 1
				if street == Draw3 && old.Abs.FinalBuckets != refined.Abs.FinalBuckets && len(refined.Abs.finalParents) > 0 {
					parent = int(refined.Abs.finalParents[bucket])
					split = children[parent]
				}
				if (street == Draw1 || street == Draw2) && old.Abs.DrawBuckets != refined.Abs.DrawBuckets && len(refined.Abs.drawParents) > 0 {
					parent = int(refined.Abs.drawParents[bucket])
					split = drawChildren[parent]
				}
				x := old.Layout.BetSlot(&old.Tree.Nodes[i], ctx, parent)
				y := refined.Layout.BetSlot(node, ctx, bucket)
				for act := int64(0); act < n; act++ {
					refined.BetRegret[y+act] = old.BetRegret[x+act] / float64(split)
					refined.BetStrat[y+act] = old.BetStrat[x+act] / float64(split)
				}
				refined.BetVisits[y] = old.BetVisits[x] / uint32(split)
				// Keep a nonempty inherited policy eligible for Extract(1).
				// This floor is prior eligibility, not a new observed visit.
				if old.BetVisits[x] > 0 && refined.BetVisits[y] == 0 {
					refined.BetVisits[y] = 1
				}
			}
		}
	}
}
