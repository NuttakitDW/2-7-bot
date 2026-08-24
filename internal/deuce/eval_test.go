package deuce

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

// hand parses a space-free five-card string like "7c5d4h3s2c".
func hand(t *testing.T, text string) []cards.Card {
	t.Helper()
	if len(text) != 2*HandSize {
		t.Fatalf("hand %q: want %d characters", text, 2*HandSize)
	}
	parsed := make([]cards.Card, 0, HandSize)
	for i := 0; i < len(text); i += 2 {
		card, err := cards.ParseCard(text[i : i+2])
		if err != nil {
			t.Fatalf("hand %q: %v", text, err)
		}
		parsed = append(parsed, card)
	}
	return parsed
}

// Every assertion the engine makes about DeuceToSevenLow, transcribed from
// third_party/poker-arena/crates/poker-core/src/eval/low.rs:174-206 at the
// pinned ENGINE_SHA. If any of these break after a SHA bump, the evaluator
// and the engine have drifted and nothing downstream can be trusted.
func TestEngineOrderingAssertions(t *testing.T) {
	tests := []struct {
		name          string
		better, worse string
	}{
		{"75432 is the nuts, over 85432", "7c5d4h3s2c", "8c5d4h3s2c"},
		{"75432 is the nuts, over 76432", "7c5d4h3s2c", "7c6d4h3s2c"},
		{"23457 beats the 23456 straight", "2c3d4h5s7c", "2c3d4h5s6c"},
		{"any nine low beats a six-high straight", "9c7d5h4s2c", "6c5d4h3s2c"},
		{"any nine low beats a king-high flush", "9c7d5h4s2c", "Kc8c6c4c2c"},
		{"a nine low beats the ace-high A5432", "9c7d5h4s2c", "5c4d3h2sAc"},
		{"A5432 is no pair, so it still beats a pair", "5c4d3h2sAc", "2c2d4h5s7c"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			better, worse := Eval(hand(t, test.better)), Eval(hand(t, test.worse))
			if better <= worse {
				t.Errorf("Eval(%s) = %d, want greater than Eval(%s) = %d",
					test.better, better, test.worse, worse)
			}
		})
	}
}

// Suits play no role beyond the flush check itself (eval/low.rs:180).
func TestSuitsOnlyMatterForTheFlushCheck(t *testing.T) {
	if got, want := Eval(hand(t, "7c5d4h3s2c")), Eval(hand(t, "7s5c4d3h2d")); got != want {
		t.Errorf("same ranks in different suits: got %d and %d", got, want)
	}
	mixed, flushed := Eval(hand(t, "7c5d4h3s2c")), Eval(hand(t, "7c5c4c3c2c"))
	if mixed <= flushed {
		t.Errorf("flush should be worse: mixed %d, flush %d", mixed, flushed)
	}
}

// The ace never completes a wheel, so A-5-4-3-2 is an ace-high no-pair hand
// rather than a straight (eval/low.rs:199-206).
func TestAceIsAlwaysHigh(t *testing.T) {
	value := Eval(hand(t, "5c4d3h2sAc"))
	if class := value.Class(); class != HighCard {
		t.Errorf("A5432 class = %v, want high card", class)
	}
	if high := value.HighCard(); high != cards.Ace {
		t.Errorf("A5432 high card = %v, want A", high)
	}
	if category := CategoryOf(value); category != Weak {
		t.Errorf("A5432 category = %v, want weak", category)
	}
}

func TestCategoryLadder(t *testing.T) {
	tests := []struct {
		name string
		hand string
		want Category
	}{
		{"the nuts", "7c5d4h3s2c", Seven},
		{"eight low", "8c5d4h3s2c", Eight},
		{"nine low", "9c7d5h4s2c", Nine},
		{"ten low", "Tc8d5h4s2c", Ten},
		{"jack low", "Jc8d5h4s2c", Weak},
		{"ace low", "5c4d3h2sAc", Weak},
		{"a pair is not a low", "2c2d4h5s7c", Broken},
		{"a straight is not a low", "6c5d4h3s2c", Broken},
		{"a flush is not a low", "Kc8c6c4c2c", Broken},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Categorize(hand(t, test.hand)); got != test.want {
				t.Errorf("Categorize(%s) = %v, want %v", test.hand, got, test.want)
			}
		})
	}
}

// The whole five-card space, checked for the textbook class frequencies. The
// engine runs the same sweep over its high evaluator
// (eval/high.rs:181-200); reproducing the counts here proves the classifier
// underneath the inversion agrees with it on all 2,598,960 hands, not just
// the handful the tables above name.
func TestExhaustiveClassFrequencies(t *testing.T) {
	if testing.Short() {
		t.Skip("exhaustive sweep skipped under -short")
	}

	// The textbook frequencies, adjusted for the disabled wheel. A-5-4-3-2
	// is a straight in ordinary poker and a no-pair hand here, so the 1020
	// unsuited wheels move from Straight to HighCard and the 4 suited ones
	// from StraightFlush to Flush:
	//
	//	Straight      10200 - 1020 = 9180
	//	StraightFlush    40 -    4 = 36
	//	HighCard    1302540 + 1020 = 1303560
	//	Flush          5108 +    4 = 5112
	want := map[Class]int{
		HighCard:      1303560,
		OnePair:       1098240,
		TwoPair:       123552,
		Trips:         54912,
		Straight:      9180,
		Flush:         5112,
		FullHouse:     3744,
		Quads:         624,
		StraightFlush: 36,
	}

	deck := make([]cards.Card, 0, 52)
	for rank := cards.Two; rank <= cards.Ace; rank++ {
		for _, suit := range []cards.Suit{cards.Clubs, cards.Diamonds, cards.Hearts, cards.Spades} {
			deck = append(deck, cards.Card{Rank: rank, Suit: suit})
		}
	}

	got := map[Class]int{}
	distinct := map[Value]struct{}{}
	five := make([]cards.Card, HandSize)
	total := 0
	for a := 0; a < 52; a++ {
		for b := a + 1; b < 52; b++ {
			for c := b + 1; c < 52; c++ {
				for d := c + 1; d < 52; d++ {
					for e := d + 1; e < 52; e++ {
						five[0], five[1], five[2], five[3], five[4] =
							deck[a], deck[b], deck[c], deck[d], deck[e]
						value := Eval(five)
						got[value.Class()]++
						distinct[value] = struct{}{}
						total++
					}
				}
			}
		}
	}

	if total != 2598960 {
		t.Fatalf("enumerated %d hands, want 2598960", total)
	}
	for class, wantCount := range want {
		if got[class] != wantCount {
			t.Errorf("%v: got %d hands, want %d", class, got[class], wantCount)
		}
	}
	// 7,462 is the number of distinct five-card equivalence classes: every
	// hand collapses to one of them once suits are dropped except for the
	// flush check. Inverting the high evaluator does not change the count.
	if len(distinct) != 7462 {
		t.Errorf("distinct hand values = %d, want 7462", len(distinct))
	}
}
