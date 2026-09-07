package cfr

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/handclass"
)

func TestEquityRollsBackMonotonically(t *testing.T) {
	a := buildAbstraction(false)
	o := NewOrdering()
	eq := NewEquity(a, o, NewTransitions(a, o))
	nuts := o.Pos[handclass.Of(cards.MustParse("7c", "5d", "4h", "3s", "2c"))]
	for street := Draw1; street <= Draw3; street++ {
		if v := eq.Value[street][nuts]; v < 0.999 {
			t.Fatalf("street %d: the nuts have equity %v", street, v)
		}
		if eq.Draws[street][nuts] != 0 {
			t.Fatalf("street %d: the nuts draw %d", street, eq.Draws[street][nuts])
		}
	}
	// A draw still to come can only help a hand that may stand pat.
	for pos := range o.Order {
		if eq.Value[Draw2][pos] < eq.Value[Draw3][pos]-1e-12 || eq.Value[Draw1][pos] < eq.Value[Draw2][pos]-1e-12 {
			t.Fatalf("position %d loses equity by having a draw left: %v %v %v", pos, eq.Value[Draw1][pos], eq.Value[Draw2][pos], eq.Value[Draw3][pos])
		}
	}
	junk := o.Pos[handclass.Of(cards.MustParse("Kc", "Qd", "Jh", "Ts", "9c"))]
	if eq.Draws[Draw1][junk] != drawClip {
		t.Fatalf("K-Q-J-T-9 draws %d on the first street, want %d", eq.Draws[Draw1][junk], drawClip)
	}
}

func TestEquityAbstractionBucketsAreDenseAndOrdered(t *testing.T) {
	a := buildAbstraction(false)
	refineEquityAbstraction(a, [3]int{160, 160, 160})
	o := NewOrdering()
	for street := Draw1; street <= Draw3; street++ {
		count := a.DrawBuckets
		if street == Draw2 {
			count = a.Draw2Buckets
		} else if street == Draw3 {
			count = a.FinalBuckets
		}
		// Classes sharing a best keep share an equity exactly, so a
		// street with many draws left has fewer distinct values than
		// buckets asked for.
		if count < 100 || count > 220 {
			t.Fatalf("street %d has %d buckets, want about 160", street, count)
		}
		seen := make([]bool, count)
		for _, id := range o.Order {
			seen[a.Bucket(street, int(id))] = true
		}
		for b, ok := range seen {
			if !ok {
				t.Fatalf("street %d bucket %d is empty", street, b)
			}
		}
	}
	// On the river the best hand sits alone at the top of its group.
	nuts := int(handclass.Of(cards.MustParse("7c", "5d", "4h", "3s", "2c")))
	second := int(handclass.Of(cards.MustParse("7c", "6d", "4h", "3s", "2c")))
	if a.Bucket(Draw3, nuts) == a.Bucket(Draw3, second) {
		t.Fatal("7-5-4-3-2 shares a river bucket with 7-6-4-3-2")
	}
	t.Log(a)
}
