package sixmax

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

func valueState(street int, hand ...string) *State {
	s := newSixState(0)
	s.Hand.Street = street
	s.Hand.Cards = cards.MustParse(hand...)
	for seat := 1; seat < SeatCount; seat++ {
		s.Hand.Seats[seat].Folded = true
	}
	return s
}

func liveDraw(s *State, seat, count int) {
	s.Hand.Seats[seat].Folded = false
	s.Hand.Seats[seat].Draws[s.Hand.Street] = count
}

func TestValueNineLeadUsesOnlyCurrentLiveDraws(t *testing.T) {
	s := valueState(Draw2, "9c", "6d", "4h", "3s", "2c")
	liveDraw(s, 1, 1)
	if !ValueNineLead(s) {
		t.Fatal("genuine nine versus one current one-card draw should lead")
	}
	s.Hand.Seats[1].Draws[Draw2] = 2
	if !ValueNineLead(s) {
		t.Fatal("genuine nine versus one current two-card draw should lead")
	}
	s.Hand.Seats[1].Draws[Draw2] = 0
	if ValueNineLead(s) {
		t.Fatal("genuine nine led into a live pat hand")
	}
	s.Hand.Seats[1].Draws[Draw2] = 1

	// A folded pat is dead information and must not suppress the lead.
	s.Hand.Seats[2].Draws[Draw2] = 0
	if !ValueNineLead(s) {
		t.Fatal("folded pat suppressed value lead")
	}

	// An all-in opponent remains live.
	s.Hand.Seats[1].AllIn = true
	if !ValueNineLead(s) {
		t.Fatal("all-in live opponent disappeared")
	}

	// A previous-street read is stale even if it was a two-card draw.
	s.Hand.Seats[1].Draws[Draw2] = -1
	s.Hand.Seats[1].Draws[Draw1] = 2
	if ValueNineLead(s) {
		t.Fatal("stale draw was treated as current")
	}
}

func TestValueNineLeadIsConservativeOnDrawOneMultiway(t *testing.T) {
	s := valueState(Draw1, "9c", "6d", "4h", "3s", "2c")
	liveDraw(s, 1, 2)
	liveDraw(s, 2, 1)
	if ValueNineLead(s) {
		t.Fatal("draw-one nine led when one of two opponents drew only one")
	}
	s.Hand.Seats[2].Draws[Draw1] = 2
	if !ValueNineLead(s) {
		t.Fatal("draw-one nine failed to lead versus two two-card draws")
	}
	s.Hand.Seats[3].Folded = false
	s.Hand.Seats[3].Draws[Draw1] = 3
	if ValueNineLead(s) {
		t.Fatal("nine led against more than two live opponents")
	}
}

func TestValueNineLeadRejectsWrongStreetAndNonNine(t *testing.T) {
	s := valueState(Draw3, "9c", "6d", "4h", "3s", "2c")
	liveDraw(s, 1, 2)
	if ValueNineLead(s) {
		t.Fatal("river nine used an early-street lead")
	}
	s.Hand.Street = Draw2
	s.Hand.Cards = cards.MustParse("8c", "6d", "4h", "3s", "2c")
	s.Hand.Seats[1].Draws[Draw2] = 2
	if ValueNineLead(s) {
		t.Fatal("eight was classified as a genuine nine")
	}
}

func TestStrongDrawLeadRequiresHeadsUpCurrentTwoDraw(t *testing.T) {
	s := valueState(Draw1, "8c", "6d", "4h", "2s", "Kc")
	liveDraw(s, 1, 2)
	if !StrongDrawLead(s) {
		t.Fatal("8642 draw failed to lead versus a current two-card draw")
	}
	s.Hand.Seats[1].Draws[Draw1] = 1
	if StrongDrawLead(s) {
		t.Fatal("8642 draw led versus a one-card draw")
	}
	s.Hand.Seats[1].Draws[Draw1] = -1
	if StrongDrawLead(s) {
		t.Fatal("8642 draw led with unknown current draw")
	}
	s.Hand.Seats[1].Draws[Draw1] = 2
	liveDraw(s, 2, 2)
	if StrongDrawLead(s) {
		t.Fatal("8642 draw led multiway")
	}
}

func TestStrongDrawLeadRejectsDrawTwoAndMadeEight(t *testing.T) {
	s := valueState(Draw2, "8c", "6d", "4h", "2s", "Kc")
	liveDraw(s, 1, 2)
	if StrongDrawLead(s) {
		t.Fatal("strong-draw lead escaped draw one")
	}
	s.Hand.Street = Draw1
	s.Hand.Seats[1].Draws[Draw1] = 2
	s.Hand.Cards = cards.MustParse("8c", "6d", "4h", "3s", "2c")
	if StrongDrawLead(s) {
		t.Fatal("made eight treated as a drawing hand")
	}
}

func TestEarlyTenDrawDiscardsOnlyTenOnDrawOneOrTwo(t *testing.T) {
	for _, street := range []int{Draw1, Draw2} {
		s := valueState(street, "Tc", "6d", "4h", "3s", "2c")
		discard, ok := EarlyTenDraw(s)
		if !ok || len(discard) != 1 || discard[0].Rank != cards.Ten {
			t.Fatalf("street %d early ten = %v, %v; want only ten", street, cards.Strings(discard), ok)
		}
	}
}

func TestEarlyTenDrawDoesNotOverrideRiverOrRoughTwoDraw(t *testing.T) {
	river := valueState(Draw3, "Tc", "6d", "4h", "3s", "2c")
	if discard, ok := EarlyTenDraw(river); ok || discard != nil {
		t.Fatalf("river override = %v, %v", cards.Strings(discard), ok)
	}
	rough := valueState(Draw2, "Tc", "9d", "7h", "6s", "2c")
	if discard, ok := EarlyTenDraw(rough); ok || discard != nil {
		t.Fatalf("rough ten override = %v, %v", cards.Strings(discard), ok)
	}
}
