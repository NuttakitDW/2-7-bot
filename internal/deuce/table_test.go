package deuce

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

// TestTableMatchesEval is what licenses every offline builder to use the table
// instead of Eval: they agree on all 2,598,960 deals, so the table inherits
// Eval's exhaustive and oracle coverage rather than needing its own.
func TestTableMatchesEval(t *testing.T) {
	table := NewTable()
	if len(table) != cards.Deals {
		t.Fatalf("table has %d entries, want %d", len(table), cards.Deals)
	}

	hand := make([]cards.Card, 0, HandSize)
	for index := 0; index < cards.Deals; index++ {
		set := cards.DealFromIndex(index)
		hand = set.Append(hand[:0])
		if got, want := table.Lookup(index), Eval(hand); got != want {
			t.Fatalf("deal %d %v: table has %#x, Eval says %#x", index, hand, got, want)
		}
		if got := table.Value(set); got != table.Lookup(index) {
			t.Fatalf("deal %d: Value and Lookup disagree", index)
		}
	}
}

func BenchmarkEval(b *testing.B) {
	hand := cards.MustParse("2c", "3d", "4h", "5s", "7c")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = Eval(hand)
	}
}

// BenchmarkTableLookup walks the whole table rather than hitting one entry, so
// the number includes the cache misses a real enumeration pays.
func BenchmarkTableLookup(b *testing.B) {
	table := NewTable()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = table.Lookup(i * 7919 % cards.Deals)
	}
}

func BenchmarkNewTable(b *testing.B) {
	for i := 0; i < b.N; i++ {
		sinkTable = NewTable()
	}
}

var (
	sink      Value
	sinkTable Table
)
