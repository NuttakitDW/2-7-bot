package cfr

import (
	"sync"

	"github.com/nuttakit/2-7-bot/internal/handclass"
)

// Use the canonical candidate keeps, independent of the running blueprint's
// bucket profile. Feature meaning must stay identical in extraction and play.
var policyEquities = sync.OnceValue(func() *[handclass.Num][4]float64 {
	a := buildAbstraction(false)
	o := NewOrdering()
	tr := NewTransitions(a, o)
	eq := NewEquity(a, o, tr)
	threeDraws, _ := rollBack(tr, eq.Value[Draw1])
	out := new([handclass.Num][4]float64)
	for pos, id := range o.Order {
		out[id] = [4]float64{eq.Value[Draw3][pos], eq.Value[Draw2][pos], eq.Value[Draw1][pos], threeDraws[pos]}
	}
	return out
})
