package policy

import (
	"strings"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/table"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// The street now matters — the input h1's one-table rule could not read.
// Both cases pin the direction the measurement found and the old rule got
// wrong: a marginal made hand breaks while draws remain and stands once
// they are gone, because the last draw is the one you cannot come back
// from. Every pinned cell was stable across independent 20M-deal
// generations.
func TestDrawIsStreetAware(t *testing.T) {
	tests := []struct {
		name     string
		spot     spot
		wantPat  bool
		wantDrop string
	}{
		{"a ten breaks a one-card read while a draw remains",
			spot{seat: 0, street: table.Draw2, hole: "Tc8d7h5s2c",
				oppDraws: map[int]int{table.Draw2: 1}}, false, "Tc"},
		{"the same ten and read stand pat on the last draw",
			spot{seat: 0, street: table.Draw3, hole: "Tc8d7h5s2c",
				oppDraws: map[int]int{table.Draw3: 1}}, true, ""},

		{"a blind nine breaks on the first draw",
			spot{seat: 1, street: table.Draw1, hole: "9c8d7h5s2c"}, false, "9c"},
		{"the same nine stands on the last draw against a one-card read",
			spot{seat: 1, street: table.Draw3, hole: "9c8d7h5s2c",
				oppDraws: map[int]int{table.Draw2: 1}}, true, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := test.spot.build(t)
			discards := Draw(&state.Hand)
			if test.wantPat != (len(discards) == 0) {
				t.Fatalf("discarded %v, wantPat %v", cards.Strings(discards), test.wantPat)
			}
			if got := strings.Join(cards.Strings(discards), ""); !test.wantPat && got != test.wantDrop {
				t.Errorf("discarded %q, want %q", got, test.wantDrop)
			}
		})
	}
}

// Staleness now matters, and Draw must carry it from OpponentDraw into the
// table — the value h1 read and threw away. The same hand, street and
// count draw differently by seat: the button, on a fresh pat read, keeps
// the four-straight and draws one at it; the big blind, whose pat read is
// a street old and who draws first, keeps the safe three.
func TestDrawReadsStaleness(t *testing.T) {
	fresh := spot{seat: 0, street: table.Draw2, hole: "3c4d5h6sKc",
		oppDraws: map[int]int{table.Draw2: 0}}.build(t)
	if got := strings.Join(cards.Strings(Draw(&fresh.Hand)), ""); got != "Kc" {
		t.Errorf("button on a fresh pat read discarded %q, want %q", got, "Kc")
	}

	stale := spot{seat: 1, street: table.Draw2, hole: "3c4d5h6sKc",
		oppDraws: map[int]int{table.Draw1: 0}}.build(t)
	if got := strings.Join(cards.Strings(Draw(&stale.Hand)), ""); got != "6sKc" {
		t.Errorf("big blind on a stale pat read discarded %q, want %q", got, "6sKc")
	}
}

// The continues() freeze: the calling-station guard still reasons from the
// structural DrawingKeep, not the generated table, so h3's betting stays
// bit-identical to h2's (see the comment in bet.go). This hand's table keep
// on a fresh pat read is the four-card 3-4-5-6 — wide enough to call on
// draw2 — while its structural keep is three cards, which folds. The fold
// is the pin: it breaks if anyone migrates continues() to the table.
func TestContinuesIgnoresTheDrawTable(t *testing.T) {
	facingBet := wire.Decision{
		Kind: wire.DecisionWager, Fold: true,
		Call: ptr(uint64(200)), Raise: &wire.Range{MinTo: 400, MaxTo: 400},
	}
	state := spot{seat: 0, street: table.Draw2, hole: "3c4d5h6sKc",
		oppDraws: map[int]int{table.Draw2: 0}, oppWagers: 1}.build(t)

	if got := strings.Join(cards.Strings(Draw(&state.Hand)), ""); got != "Kc" {
		t.Fatalf("table keep changed (discarded %q): pick a new pinning hand", got)
	}
	if got := Decide(state, facingBet).Kind; got != wire.ActionFold {
		t.Errorf("action = %q, want %q", got, wire.ActionFold)
	}
}

// The properties any generated draw table must satisfy, whatever sample
// volume produced it — the draw-side mirror of TestChartTableProperties.
func TestDrawTableProperties(t *testing.T) {
	reads := []Read{{}}
	for _, streetsAgo := range []int{0, 1} {
		for count := 0; count <= 2; count++ {
			reads = append(reads, Read{Count: count, StreetsAgo: streetsAgo, Known: true})
		}
	}

	for id := handclass.ID(0); id < handclass.Num; id++ {
		if handclass.Weight(id) == 0 {
			continue
		}
		hand := handclass.Representative(id)
		category := deuce.Categorize(hand)
		held := cards.NewSet(hand)

		for street := table.Draw1; street <= table.Draw3; street++ {
			for _, read := range reads {
				discards := DrawDiscards(hand, street, read)

				// Legality: only held cards, each at most once.
				thrown := cards.NewSet(discards)
				if thrown.Len() != len(discards) || held.Len() != 5 {
					t.Fatalf("class %d street %d %+v: discards %v repeat a card",
						id, street, read, cards.Strings(discards))
				}
				for _, card := range discards {
					if !held.Has(card) {
						t.Fatalf("class %d street %d %+v: discarding %v, not held",
							id, street, read, card)
					}
				}

				// A made seven never breaks, in any context.
				if category == deuce.Seven && len(discards) != 0 {
					t.Fatalf("class %d street %d %+v: a seven discarded %v",
						id, street, read, cards.Strings(discards))
				}
				// A dead hand never stands: the table does not snow. The
				// same deliberate omission h1 documents in draw.go, held
				// as a property of the generated data.
				if category == deuce.Broken && len(discards) == 0 {
					t.Fatalf("class %d street %d %+v: stood pat on a broken hand",
						id, street, read)
				}
			}
		}
	}
}
