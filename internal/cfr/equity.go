package cfr

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// equityProfile sizes the equity abstraction's betting buckets per
// street with draws still to come, then one, then none: "draw1,draw2,draw3".
// A street ends up with at least that many buckets, and a few more where
// the split by draw count leaves remainders.
var equityProfile = "160,160,160"

func parseEquityProfile(text string) [3]int {
	var counts [3]int
	fields := strings.Split(text, ",")
	if len(fields) != 3 {
		panic("CFR equity profile wants three bucket counts: " + text)
	}
	for i, field := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || n < 1 {
			panic("CFR equity profile: bad bucket count " + field)
		}
		counts[i] = n
	}
	return counts
}

// Equity is the abstraction's reading of every class's strength on each
// betting street, by strength position: the showdown equity a hand
// would have against a uniformly dealt opponent after drawing its best
// candidate keep on every draw still to come, and how many cards that
// keep draws now.
type Equity struct {
	Order *Ordering
	// Value[street][pos], for the three betting streets after a draw.
	Value [Streets][]float64
	// Draws[street][pos] is the cards the best keep draws on the next
	// draw, clipped, or 0 after the last.
	Draws [Streets][]uint8
}

// NewEquity rolls the showdown ordering back through the draws. It is a
// static opponent's equity — the opponent neither draws nor bets — which
// is what makes it a ranking rather than a value.
func NewEquity(a *Abstraction, o *Ordering, tr *Transitions) *Equity {
	n := len(o.Order)
	e := &Equity{Order: o}
	showdown := make([]float64, n)
	// Equity at showdown: mass strictly below plus half the ties.
	cum := make([]float64, n+1)
	for pos, w := range o.Weight {
		cum[pos+1] = cum[pos] + w
	}
	for pos := range showdown {
		lo, hi := o.Lo[pos], o.Hi[pos]
		showdown[pos] = cum[lo] + (cum[hi]-cum[lo])/2
	}
	e.Value[Draw3] = showdown
	e.Draws[Draw3] = make([]uint8, n)
	for street := Draw2; street >= Draw1; street-- {
		e.Value[street], e.Draws[street] = rollBack(tr, e.Value[street+1])
	}
	return e
}

// rollBack gives each class its best candidate's expected next-street
// value, dotting each transition table once.
func rollBack(tr *Transitions, next []float64) ([]float64, []uint8) {
	n := len(next)
	value, draws := make([]float64, n), make([]uint8, n)
	dots := make([]float64, len(tr.Tables))
	for tid, t := range tr.Tables {
		sum := 0.0
		for j, idx := range t.Idx {
			sum += t.P[j] * next[idx]
		}
		dots[tid] = sum
	}
	for pos := range value {
		for i := 0; i < int(tr.NumCand[pos]); i++ {
			var v float64
			if tid := tr.Cand[pos][i]; tid == Pat {
				v = next[pos]
			} else {
				v = dots[tid]
			}
			if i == 0 || v > value[pos] {
				value[pos], draws[pos] = v, uint8(clip(int(tr.Count[pos][i])))
			}
		}
	}
	return value, draws
}

// refineEquityAbstraction replaces the hand-written betting buckets with
// equity quantiles per street: classes are first split by how many
// cards their best keep draws, so a made hand never shares a bucket with
// a draw of the same equity, then each group is cut into buckets of
// equal measure, half by class count and half by deal weight, so the
// rare strong hands at the top get as much resolution as the common
// junk at the bottom. Classes of equal equity are never split.
func refineEquityAbstraction(a *Abstraction, counts [3]int) {
	o := NewOrdering()
	tr := NewTransitions(a, o)
	eq := NewEquity(a, o, tr)
	for street := Draw1; street <= Draw3; street++ {
		buckets := bucketize(eq.Value[street], eq.Draws[street], o.Weight, counts[street-Draw1])
		total := 0
		for _, b := range buckets {
			total = max(total, int(b)+1)
		}
		for pos, id := range o.Order {
			info := &a.Classes[id]
			switch street {
			case Draw1:
				info.Draw = buckets[pos]
			case Draw2:
				info.Draw2 = buckets[pos]
			default:
				info.Final = buckets[pos]
			}
		}
		switch street {
		case Draw1:
			a.DrawBuckets = total
		case Draw2:
			a.Draw2Buckets = total
		default:
			a.FinalBuckets = total
		}
	}
}

// bucketize cuts positions into about k buckets: first by group, each
// group getting buckets in proportion to its measure, then by descending
// score within the group, never splitting a tie.
func bucketize(score []float64, group []uint8, weight []float64, k int) []uint16 {
	n := len(score)
	out := make([]uint16, n)
	members := map[uint8][]int{}
	for pos := 0; pos < n; pos++ {
		members[group[pos]] = append(members[group[pos]], pos)
	}
	groups := make([]uint8, 0, len(members))
	for g := range members {
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i] < groups[j] })
	next := 0
	for _, g := range groups {
		positions := members[g]
		sort.SliceStable(positions, func(i, j int) bool { return score[positions[i]] > score[positions[j]] })
		mass := 0.0
		for _, pos := range positions {
			mass += weight[pos]
		}
		share := (float64(len(positions))/float64(n) + mass) / 2
		want := max(minBucketsPerGroup, int(float64(k)*share+0.5))
		next = cut(out, positions, score, weight, mass, want, next)
	}
	return out
}

// minBucketsPerGroup keeps a small draw-count group from collapsing into
// a single bucket.
const minBucketsPerGroup = 4

// cut assigns consecutive bucket ids from first to positions already
// sorted by descending score, and returns the next free id. Bucket sizes
// ramp linearly from the top: the best bucket holds a sliver of the
// group's measure and the worst a fat slice, so the strong hands, where
// the money changes hands, are told apart and the junk is lumped.
func cut(out []uint16, positions []int, score, weight []float64, mass float64, want, first int) int {
	ramp := float64(want) * float64(want+1) / 2
	target := func(bucket int) float64 { return float64(bucket-first+1) / ramp }
	bucket, filled := first, 0.0
	for i := 0; i < len(positions); {
		j := i
		for j < len(positions) && score[positions[j]] == score[positions[i]] {
			j++
		}
		tie := 0.0
		for _, pos := range positions[i:j] {
			tie += weight[pos] / mass / 2
		}
		tie += float64(j-i) / float64(len(positions)) / 2
		if filled > 0 && filled+tie > target(bucket) && bucket-first < want-1 {
			bucket++
			filled = 0
		}
		for _, pos := range positions[i:j] {
			out[pos] = uint16(bucket)
		}
		filled += tie
		i = j
	}
	return bucket + 1
}

// String summarises the bucket counts, for logs.
func (a *Abstraction) String() string {
	return fmt.Sprintf("buckets: draw1 %d, draw2 %d, river %d", a.DrawBuckets, max(a.Draw2Buckets, a.DrawBuckets), a.FinalBuckets)
}
