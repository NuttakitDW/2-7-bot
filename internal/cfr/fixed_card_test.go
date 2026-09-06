package cfr

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

func set(texts ...string) cards.Set { return cards.NewSet(cards.MustParse(texts...)) }

func TestFixedCardIdentifiesTheConstantBigBlindCard(t *testing.T) {
	var f FixedCard
	f.ObserveBigBlind(set("Qh", "2c", "7h", "7s", "4s"))
	f.ObserveBigBlind(set("Qh", "9d", "5s", "3d", "Kc"))
	if f.OpponentHolds() != 0 {
		t.Fatal("two hands are not enough evidence")
	}
	f.ObserveBigBlind(set("Qh", "Ac", "Jd", "6h", "8s"))
	if f.OpponentHolds() != set("Qh") {
		t.Fatalf("held %v", f.OpponentHolds())
	}
	if f.Group() != FixedGroup(cards.Queen) || f.Group() != 8 {
		t.Fatalf("group %d", f.Group())
	}
	// A hand without the card resets the search instead of leaving it empty.
	f.ObserveBigBlind(set("2d", "3d", "4d", "5d", "9c"))
	if f.OpponentHolds() != 0 || f.candidates != set("2d", "3d", "4d", "5d", "9c") {
		t.Fatalf("after reset: held %v candidates %v", f.OpponentHolds(), f.candidates)
	}
}

func TestFixedCardNeedsASingleSurvivor(t *testing.T) {
	var f FixedCard
	for i := 0; i < 4; i++ {
		f.ObserveBigBlind(set("Qh", "2c", "7h", "7s", "4s"))
	}
	if f.OpponentHolds() != 0 {
		t.Fatal("five candidates cannot name a card")
	}
}
