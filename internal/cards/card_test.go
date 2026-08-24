package cards

import (
	"encoding/json"
	"strings"
	"testing"
)

// Every card in the deck must survive the wire form unchanged, because a
// card that mis-parses becomes a discard the arena rejects — and under the
// default fault policy that is silently substituted rather than reported.
func TestEveryCardRoundTripsThroughTheWireForm(t *testing.T) {
	suits := []Suit{Clubs, Diamonds, Hearts, Spades}
	seen := map[string]bool{}
	for rank := Two; rank <= Ace; rank++ {
		for _, suit := range suits {
			card := Card{Rank: rank, Suit: suit}
			text := card.String()
			if len(text) != 2 {
				t.Fatalf("%v renders as %q, want two characters", card, text)
			}
			if seen[text] {
				t.Fatalf("%q rendered twice", text)
			}
			seen[text] = true

			parsed, err := ParseCard(text)
			if err != nil {
				t.Fatalf("ParseCard(%q): %v", text, err)
			}
			if parsed != card {
				t.Errorf("ParseCard(%q) = %v, want %v", text, parsed, card)
			}

			encoded, err := json.Marshal(card)
			if err != nil {
				t.Fatalf("marshal %v: %v", card, err)
			}
			var decoded Card
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatalf("unmarshal %s: %v", encoded, err)
			}
			if decoded != card {
				t.Errorf("JSON round trip of %v gave %v", card, decoded)
			}
		}
	}
	if len(seen) != 52 {
		t.Errorf("rendered %d distinct cards, want 52", len(seen))
	}
}

// The engine writes every tiebreak nibble in this space (eval/mod.rs:11-20).
func TestRankIndexMatchesTheEngineEncoding(t *testing.T) {
	if got := Two.Index(); got != 0 {
		t.Errorf("Two.Index() = %d, want 0", got)
	}
	if got := Ace.Index(); got != 12 {
		t.Errorf("Ace.Index() = %d, want 12", got)
	}
}

func TestParseCardRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name, text, wantErr string
	}{
		{"empty", "", "two characters"},
		{"one character", "A", "two characters"},
		{"three characters", "10c", "two characters"},
		{"unknown rank", "1c", "unknown rank"},
		{"unknown suit", "Ax", "unknown suit"},
		{"reversed", "cA", "unknown rank"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseCard(test.text)
			if err == nil {
				t.Fatalf("ParseCard(%q) succeeded", test.text)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, test.wantErr)
			}
		})
	}
}

// The protocol only ever emits "As" style, but a mis-cased card would
// otherwise turn into a fault for no reason.
func TestParseCardAcceptsEitherCase(t *testing.T) {
	for _, text := range []string{"As", "as", "AS", "aS"} {
		card, err := ParseCard(text)
		if err != nil {
			t.Fatalf("ParseCard(%q): %v", text, err)
		}
		if card.String() != "As" {
			t.Errorf("ParseCard(%q) = %v, want As", text, card)
		}
	}
}

func TestHandHelpers(t *testing.T) {
	hand := MustParse("9d", "2c", "Ks", "2h", "8h")

	t.Run("sorts deuce first and leaves the input alone", func(t *testing.T) {
		before := Strings(hand)
		got := strings.Join(Strings(SortedByRank(hand)), " ")
		if want := "2c 2h 8h 9d Ks"; got != want {
			t.Errorf("sorted = %q, want %q", got, want)
		}
		if after := Strings(hand); strings.Join(after, " ") != strings.Join(before, " ") {
			t.Errorf("the input was reordered: %v", after)
		}
	})

	t.Run("deduplicates ranks", func(t *testing.T) {
		got := DistinctRanks(hand)
		if len(got) != 4 || got[0] != Two || got[3] != King {
			t.Errorf("DistinctRanks = %v", got)
		}
	})

	t.Run("removes one copy per listed card", func(t *testing.T) {
		got := strings.Join(Strings(Without(hand, MustParse("2c", "Ks"))), " ")
		if want := "9d 2h 8h"; got != want {
			t.Errorf("Without = %q, want %q", got, want)
		}
	})

	t.Run("ignores cards the hand does not hold", func(t *testing.T) {
		got := Without(hand, MustParse("Ac"))
		if len(got) != len(hand) {
			t.Errorf("Without dropped something: %v", Strings(got))
		}
	})

	t.Run("a draw is a removal then an addition", func(t *testing.T) {
		got := With(Without(hand, MustParse("Ks", "9d")), MustParse("3s", "4d"))
		if want := "2c 2h 8h 3s 4d"; strings.Join(Strings(got), " ") != want {
			t.Errorf("after the draw: %q, want %q", strings.Join(Strings(got), " "), want)
		}
	})
}

func TestSameSuit(t *testing.T) {
	tests := []struct {
		name string
		hand []Card
		want bool
	}{
		{"a flush", MustParse("2c", "5c", "9c", "Jc", "Kc"), true},
		{"one card off", MustParse("2c", "5c", "9c", "Jc", "Kd"), false},
		{"a single card", MustParse("2c"), true},
		{"nothing", nil, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SameSuit(test.hand); got != test.want {
				t.Errorf("SameSuit = %v, want %v", got, test.want)
			}
		})
	}
}
