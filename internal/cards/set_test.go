package cards

import "testing"

func TestCardIndexRoundTrip(t *testing.T) {
	seen := make(map[Card]bool, DeckSize)
	for index := 0; index < DeckSize; index++ {
		card := CardFromIndex(index)
		if got := card.Index(); got != index {
			t.Fatalf("CardFromIndex(%d) = %v, whose Index is %d", index, card, got)
		}
		if seen[card] {
			t.Fatalf("index %d repeats card %v", index, card)
		}
		seen[card] = true
	}
	if len(seen) != DeckSize {
		t.Fatalf("indices cover %d cards, want %d", len(seen), DeckSize)
	}
}

func TestSetOperations(t *testing.T) {
	hand := MustParse("2c", "3d", "4h", "5s", "7c")
	set := NewSet(hand)

	if set.Len() != 5 {
		t.Fatalf("Len = %d, want 5", set.Len())
	}
	if set.Complement().Len() != DeckSize-5 {
		t.Fatalf("Complement Len = %d, want %d", set.Complement().Len(), DeckSize-5)
	}
	for _, card := range hand {
		if !set.Has(card) {
			t.Fatalf("set is missing %v", card)
		}
	}

	// Add and Remove must not touch the receiver.
	seven := MustParse("7c")[0]
	if dropped := set.Remove(seven); dropped.Has(seven) || set.Len() != 5 {
		t.Fatalf("Remove mutated the receiver or failed to drop %v", seven)
	}

	got := set.Append(nil)
	if len(got) != 5 {
		t.Fatalf("Append returned %d cards, want 5", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Index() >= got[i].Index() {
			t.Fatalf("Append is not ascending: %v then %v", got[i-1], got[i])
		}
	}
}

// TestDealIndexIsABijection is the check the whole abstraction rests on: every
// five-card deal must have exactly one index in [0, Deals), because the bucket
// tables are flat arrays keyed on it.
func TestDealIndexIsABijection(t *testing.T) {
	seen := make([]bool, Deals)
	count := 0
	for e := 4; e < DeckSize; e++ {
		for d := 3; d < e; d++ {
			for c := 2; c < d; c++ {
				for b := 1; b < c; b++ {
					for a := 0; a < b; a++ {
						set := Set(1)<<uint(a) | Set(1)<<uint(b) | Set(1)<<uint(c) |
							Set(1)<<uint(d) | Set(1)<<uint(e)
						index := set.DealIndex()
						if seen[index] {
							t.Fatalf("index %d claimed twice, at deal %d", index, count)
						}
						seen[index] = true

						// The nested loops walk colex order, so the index must
						// simply count up.
						if index != count {
							t.Fatalf("deal %d has index %d; colex order is broken", count, index)
						}
						if back := DealFromIndex(index); back != set {
							t.Fatalf("DealFromIndex(%d) = %#x, want %#x", index, back, set)
						}
						count++
					}
				}
			}
		}
	}
	if count != Deals {
		t.Fatalf("enumerated %d deals, want %d", count, Deals)
	}
}

func BenchmarkDealIndex(b *testing.B) {
	set := NewSet(MustParse("2c", "3d", "4h", "5s", "7c"))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkInt = set.DealIndex()
	}
}

func BenchmarkDealFromIndex(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkSet = DealFromIndex(i % Deals)
	}
}

var (
	sinkInt int
	sinkSet Set
)
