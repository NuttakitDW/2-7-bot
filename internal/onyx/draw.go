package onyx

import (
	"sort"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/policy"
)

type weightedValue struct {
	Value uint32
	Count int
}
type handRange struct {
	values []weightedValue
	prefix []float64
	total  float64
}

func makeRange(data []weightedValue) handRange {
	r := handRange{values: data, prefix: make([]float64, len(data)+1)}
	for i, v := range data {
		r.prefix[i+1] = r.prefix[i] + float64(v.Count)
	}
	r.total = r.prefix[len(data)]
	return r
}

func (r handRange) equity(v deuce.Value) float64 {
	if r.total == 0 {
		return 0.5
	}
	i := sort.Search(len(r.values), func(i int) bool { return r.values[i].Value >= uint32(v) })
	wins := r.prefix[i]
	if i < len(r.values) && r.values[i].Value == uint32(v) {
		wins += float64(r.values[i].Count) * 0.5
	}
	return wins / r.total
}

var finalRanges = func() (out [16]handRange) {
	for i, d := range rangeData {
		out[i] = makeRange(d)
	}
	return
}()

// finalDraw prices zero, one and two-card draws against the observed final
// range. The out-of-position player uses the previous count, not a future
// draw event. Opponent card removal and future betting are approximations.
func (b *Bot) finalDraw() []cards.Card {
	h := &b.Table.Hand
	fallback := draw(h)
	if len(fallback) > 2 {
		return fallback
	}
	count, ago, known := h.OpponentDraw(h.Street)
	if !known {
		return fallback
	}
	context := min(count, 3)
	if ago > 0 {
		context += 4
	}
	r := finalRanges[context]
	if r.total < 20 {
		return fallback
	}
	best := fallback
	value := b.drawEquity(best, r)
	for _, keep := range policy.DrawCandidates(h.Cards) {
		discard := policy.Discards(h.Cards, keep)
		if len(discard) > 2 {
			continue
		}
		candidate := b.drawEquity(discard, r)
		// Require a small improvement to avoid switching on sampling noise.
		if candidate > value+0.015 {
			value = candidate
			best = discard
		}
	}
	return best
}

func (b *Bot) drawEquity(discard []cards.Card, r handRange) float64 {
	if len(discard) == 0 {
		return r.equity(deuce.Eval(b.Table.Hand.Cards))
	}
	if len(discard) > 2 {
		return 0
	}
	keep := cards.Without(b.Table.Hand.Cards, discard)
	deck := (cards.NewSet(b.Table.Hand.Cards) | b.muck).Complement().Append(nil)
	var hand [5]cards.Card
	copy(hand[:], keep)
	wins, n := 0.0, 0
	for i, c := range deck {
		hand[len(keep)] = c
		if len(discard) == 1 {
			wins += r.equity(deuce.Eval(hand[:]))
			n++
			continue
		}
		for _, d := range deck[i+1:] {
			hand[4] = d
			wins += r.equity(deuce.Eval(hand[:]))
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return wins / float64(n)
}
