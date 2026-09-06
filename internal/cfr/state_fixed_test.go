package cfr

import (
	"math/rand/v2"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

func TestDealFixedPutsCardInBigBlindHand(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	fixed := cards.MustParse("Qh")[0]
	var s State
	for i := 0; i < 200; i++ {
		s.DealFixed(rng, fixed)
		if cards.NewSet(s.Hands[BB][:])&cards.NewSet([]cards.Card{fixed}) == 0 {
			t.Fatalf("deal %d: big blind %v lacks %v", i, s.Hands[BB], fixed)
		}
		if cards.NewSet(s.Hands[Btn][:])&cards.NewSet([]cards.Card{fixed}) != 0 {
			t.Fatalf("deal %d: button holds the fixed card", i)
		}
		seen := cards.Set(0)
		for _, c := range s.Deck {
			seen |= cards.NewSet([]cards.Card{c})
		}
		if seen.Len() != cards.DeckSize {
			t.Fatal("deck lost a card")
		}
		s.Reset()
		if cards.NewSet(s.Hands[BB][:])&cards.NewSet([]cards.Card{fixed}) == 0 {
			t.Fatal("reset moved the fixed card")
		}
	}
}
