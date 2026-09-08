package onyx

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/table"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestRefineLearnedRaise(t *testing.T) {
	call := uint64(200)
	d := wire.Decision{Kind: wire.DecisionWager, Call: &call, Fold: true, Raise: &wire.Range{MinTo: 600, MaxTo: 600}}
	for _, tt := range []struct {
		name           string
		hand           []cards.Card
		street, wagers int
		want           string
	}{
		{"rough seven facing raise", cards.MustParse("7c", "6d", "4h", "3s", "2c"), table.Draw3, 2, wire.ActionCall},
		{"eight facing third bet", cards.MustParse("8c", "6d", "4h", "3s", "2c"), table.Draw2, 3, wire.ActionCall},
		{"nuts retain value raise", cards.MustParse("7c", "5d", "4h", "3s", "2c"), table.Draw3, 2, wire.ActionRaise},
		{"first raise retained", cards.MustParse("7c", "6d", "4h", "3s", "2c"), table.Draw3, 1, wire.ActionRaise},
		{"predraw untouched", cards.MustParse("7c", "6d", "4h", "3s", "2c"), table.Predraw, 3, wire.ActionRaise},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := &Bot{Table: table.New()}
			b.Table.Hand.Cards, b.Table.Hand.Street, b.Table.Hand.Wagers = tt.hand, tt.street, tt.wagers
			got := b.RefineLearned(d, wire.Raise(600), "raise-aware")
			if got.Kind != tt.want {
				t.Fatalf("got %s want %s", got.Kind, tt.want)
			}
			if got := b.RefineLearned(d, wire.Call(), "raise-aware"); got.Kind != wire.ActionCall {
				t.Fatal("changed a passive decision")
			}
			if got := b.RefineLearned(d, wire.Raise(600), "baseline"); got.Kind != wire.ActionRaise {
				t.Fatal("changed baseline")
			}
		})
	}
}
