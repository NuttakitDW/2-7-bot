package cfr

import (
	"fmt"
	"math"
)

// SeedBlueprint initializes a fresh learner with a policy prior. Positive
// regrets reproduce that policy under regret matching; strategy sums preserve
// it until new samples arrive. This is an empirical warm start, not recovery of
// the original regrets or a convergence guarantee. Use an already-selected
// blueprint if the deployed policy purifies its training probabilities.
// Exporting again can shift a few byte units through floating-point rounding
// and the existing quantizer; the in-memory policy retains the prior mixture.
// A single-group blueprint can also initialize every group of a fixed-card
// layout with the same abstraction, so no group starts from a random policy.
//
// Nonempty prior sets receive visit count 1 solely to remain eligible for
// Extract(1), matching refined-state priors. This is not a sampled observation.
func (tr *Trainer) SeedBlueprint(bp *Blueprint, strength float64) error {
	if math.IsNaN(strength) || math.IsInf(strength, 0) || strength <= 0 {
		return fmt.Errorf("prior strength must be finite and positive")
	}
	if bp == nil {
		return fmt.Errorf("prior blueprint is nil")
	}
	fromBase := tr.Layout.FixedGroups > 1 && int64(len(bp.Bet)) == tr.Layout.baseBet && int64(len(bp.Draw)) == tr.Layout.baseDraw
	if !fromBase && (int64(len(bp.Bet)) != tr.Layout.BetSlots || int64(len(bp.Draw)) != tr.Layout.DrawSlots) {
		return fmt.Errorf("prior blueprint does not match trainer layout")
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.Iterations() != 0 {
		return fmt.Errorf("blueprint prior requires a fresh trainer")
	}
	clear(tr.BetRegret)
	clear(tr.BetStrat)
	clear(tr.DrawRegret)
	clear(tr.DrawStrat)
	clear(tr.BetVisits)
	clear(tr.DrawVisits)
	seed := func(probs []byte, regrets, strategy []float64) bool {
		total := 0
		for _, b := range probs {
			total += int(b)
		}
		if total == 0 {
			return false
		}
		for i, b := range probs {
			value := (float64(b) / float64(total)) * strength
			regrets[i], strategy[i] = value, value
		}
		return true
	}
	seen := make(map[int64]bool)
	for group := 0; group < tr.Layout.FixedGroups; group++ {
		for i := range tr.Tree.Nodes {
			node := &tr.Tree.Nodes[i]
			if node.Kind != KindBet || (group > 0 && !tr.Layout.Sliced(node, group)) {
				continue
			}
			street, n := int(node.Street), int64(len(node.Acts))
			start := tr.Layout.BetSlotFixed(node, 0, 0, group)
			if seen[start] {
				continue
			}
			seen[start] = true
			end := start + int64(BetContexts(street)*tr.Layout.Buckets(street))*n
			sourceStart := start
			if fromBase {
				sourceStart = node.Offset
			}
			for slot := start; slot < end; slot += n {
				source := sourceStart + slot - start
				if seed(bp.Bet[source:source+n], tr.BetRegret[slot:slot+n], tr.BetStrat[slot:slot+n]) {
					tr.BetVisits[slot] = 1
				}
			}
		}
	}
	for slot := int64(0); slot < tr.Layout.DrawSlots; slot += MaxCand {
		source := slot
		if fromBase && slot >= tr.Layout.baseDraw {
			if tr.Layout.early {
				source = (slot - tr.Layout.baseDraw) % tr.Layout.earlyDraw
			} else {
				source = slot % tr.Layout.baseDraw
			}
		}
		if seed(bp.Draw[source:source+MaxCand], tr.DrawRegret[slot:slot+MaxCand], tr.DrawStrat[slot:slot+MaxCand]) {
			tr.DrawVisits[slot] = 1
		}
	}
	return nil
}
