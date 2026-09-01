package policy

import (
	"strings"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/handclass"
)

// The candidate lists the draw table indexes into. Cases mirror the shapes
// TestClassify pins: straight traps, flushes, pairs, the deuce. What matters
// is that the list is deterministic, starts with the stand pat, and offers
// the alternates the one-keep rule cannot express — the untrimmed
// straight-risk keep and the one-shorter keep.
func TestDrawCandidates(t *testing.T) {
	tests := []struct {
		name string
		hand string
		want []string // rank lists, candidate 0 first
	}{
		// The structural keep for a pat hand is the break-to-four; the
		// shorter keep follows.
		{"the nuts", "7c5d4h3s2c", []string{"23457", "2345", "234"}},
		{"a made eight", "8c5d4h3s2c", []string{"23458", "2345", "234"}},
		{"a rough nine", "9c8d7h5s2c", []string{"25789", "2578", "257"}},

		// An open-ender's structural keep is trimmed to three; the untrimmed
		// four-card keep is the alternate the table can now measure.
		{"an open-ended four-straight", "3c4d5h6sKc", []string{"3456K", "345", "34", "3456"}},
		{"a middle three-straight", "5c6d7hKsQc", []string{"567QK", "56", "567"}},

		// 2-3-4-5 is a one-ender, so trimmed and untrimmed coincide.
		{"a one-ender", "2c3d4h5sKc", []string{"2345K", "2345", "234"}},
		{"a six-high straight flush shape", "2c3d4h5s6c", []string{"23456", "2345", "234"}},

		// The stand pat is the only candidate that keeps a pair copy.
		{"a pair", "2c2d4h5s7c", []string{"22457", "2457", "245"}},

		{"a deuce and a low card", "2c7dKhQsJc", []string{"27JQK", "27"}},
		{"no deuce and no low cards", "AcKdQhJs9c", []string{"9JQKA", "9"}},
		{"nothing nine or under draws five", "TcJdQhKsAc", []string{"TJQKA", ""}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DrawCandidates(parseHand(t, test.hand))
			if len(got) != len(test.want) {
				t.Fatalf("candidates = %v, want %v", rankLists(got), test.want)
			}
			for i, want := range test.want {
				if ranks(got[i]) != want {
					t.Errorf("candidate %d = %q, want %q", i, ranks(got[i]), want)
				}
			}
		})
	}
}

// The properties every class's candidate list must satisfy, whatever hand
// represents it: candidate 0 stands pat, no other candidate keeps a pair
// copy or exceeds the previous keep... and the list is small enough for the
// table's index width.
func TestDrawCandidateProperties(t *testing.T) {
	for id := handclass.ID(0); id < handclass.Num; id++ {
		if handclass.Weight(id) == 0 {
			continue
		}
		hand := handclass.Representative(id)
		list := DrawCandidates(hand)

		if len(list) == 0 || len(list) > MaxDrawCandidates {
			t.Fatalf("class %d: %d candidates, want 1..%d", id, len(list), MaxDrawCandidates)
		}
		if len(Discards(hand, list[0])) != 0 {
			t.Fatalf("class %d: candidate 0 %q discards %v, want a stand pat",
				id, ranks(list[0]), cards.Strings(Discards(hand, list[0])))
		}
		seen := map[string]bool{}
		for i, keep := range list {
			if seen[ranks(keep)] {
				t.Fatalf("class %d: candidate %d %q repeats", id, i, ranks(keep))
			}
			seen[ranks(keep)] = true
			if i == 0 {
				continue
			}
			if len(keep) == 0 {
				// The draw-five, legal only where the structural rule
				// itself keeps nothing.
				if i != 1 || len(DrawingKeep(hand)) != 0 {
					t.Fatalf("class %d: candidate %d keeps nothing", id, i)
				}
				continue
			}
			for j := 1; j < len(keep); j++ {
				if keep[j] <= keep[j-1] {
					t.Fatalf("class %d: candidate %d %q keeps a pair copy or is unsorted",
						id, i, ranks(keep))
				}
			}
		}
	}
}

func rankLists(lists [][]cards.Rank) string {
	out := make([]string, len(lists))
	for i, list := range lists {
		out[i] = ranks(list)
	}
	return strings.Join(out, " ")
}
