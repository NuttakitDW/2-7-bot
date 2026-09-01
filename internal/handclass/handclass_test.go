package handclass

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

// Every ID must produce a representative that maps back to itself, and the
// representative must be a well-formed deal. The thirteen five-of-a-kind
// multisets no deck can deal are the only dead entries.
func TestRepresentativeRoundTrips(t *testing.T) {
	dead := 0
	for id := ID(0); id < Num; id++ {
		if Weight(id) == 0 {
			dead++
			continue
		}
		hand := Representative(id)
		if set := cards.NewSet(hand); set.Len() != 5 {
			t.Fatalf("id %d: representative %v holds duplicate cards", id, cards.Strings(hand))
		}
		if got := Of(hand); got != id {
			t.Fatalf("Of(Representative(%d)) = %d", id, got)
		}
	}
	if dead != 13 {
		t.Errorf("%d zero-weight classes, want the 13 five-of-a-kinds", dead)
	}
}

// The classes must partition the deals exactly: walking all C(52,5) deals,
// every class must be hit precisely Weight times.
func TestWeightsPartitionTheDeck(t *testing.T) {
	if testing.Short() {
		t.Skip("walks all 2,598,960 deals")
	}
	counts := make([]int, Num)
	hand := make([]cards.Card, 0, 5)
	for deal := 0; deal < cards.Deals; deal++ {
		hand = cards.DealFromIndex(deal).Append(hand[:0])
		counts[Of(hand)]++
	}
	total := 0
	for id, count := range counts {
		if want := Weight(ID(id)); count != want {
			t.Fatalf("id %d (%v): counted %d deals, Weight says %d",
				id, cards.Strings(Representative(ID(id))), count, want)
		}
		total += count
	}
	if total != cards.Deals {
		t.Fatalf("counted %d deals in all, want %d", total, cards.Deals)
	}
}

// Suits must be invisible except for the flush bit.
func TestSuitsOnlyCarryTheFlushBit(t *testing.T) {
	mixed := cards.MustParse("7c", "5d", "4h", "3s", "2c")
	reshuffled := cards.MustParse("7d", "5c", "4s", "3h", "2d")
	flush := cards.MustParse("7c", "5c", "4c", "3c", "2c")

	if Of(mixed) != Of(reshuffled) {
		t.Errorf("same ranks, both non-flush: Of %d != %d", Of(mixed), Of(reshuffled))
	}
	if Of(mixed) == Of(flush) {
		t.Errorf("flush and non-flush share id %d", Of(mixed))
	}
	if Weight(Of(flush)) != 4 {
		t.Errorf("flush class weight = %d, want 4", Weight(Of(flush)))
	}
}

func BenchmarkOf(b *testing.B) {
	hand := cards.MustParse("9c", "8d", "7h", "5s", "2c")
	for i := 0; i < b.N; i++ {
		Of(hand)
	}
}
