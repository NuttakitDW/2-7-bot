package cfr

import (
	"fmt"
	"math"
)

// Discount reduces the influence of past regret and strategy samples equally.
// It waits for active iterations and excludes snapshots until every table has
// been scaled. Visit counts remain actual observations, for extraction pruning.
func (tr *Trainer) Discount(factor float64) error {
	if math.IsNaN(factor) || math.IsInf(factor, 0) || factor <= 0 || factor > 1 {
		return fmt.Errorf("discount factor must be in (0,1]")
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	var betStart, drawStart int64
	if tr.FixedOnly {
		betStart, drawStart = tr.Layout.baseBet, tr.Layout.baseDraw
	}
	for _, values := range [][]float64{tr.BetRegret[betStart:], tr.BetStrat[betStart:], tr.DrawRegret[drawStart:], tr.DrawStrat[drawStart:]} {
		for i := range values {
			values[i] *= factor
		}
	}
	return nil
}
