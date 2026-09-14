package zircon

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/policy"
)

// DrawOutcomes exactly enumerates every replacement card still possible from
// the hero's visible five cards. Discards from earlier draws are intentionally
// absent: triple draw reshuffles the muck when the deck is exhausted.
type DrawOutcomes struct {
	Total, Seven, Eight, Nine, Better int
}

func EnumerateOneCard(hand []cards.Card, discard cards.Card) DrawOutcomes {
	if len(hand) != deuce.HandSize {
		return DrawOutcomes{}
	}
	held := cards.NewSet(hand)
	base := make([]cards.Card, 0, deuce.HandSize)
	removed := false
	for _, card := range hand {
		if !removed && card == discard {
			removed = true
			continue
		}
		base = append(base, card)
	}
	if !removed {
		return DrawOutcomes{}
	}
	current := deuce.Eval(hand)
	var out DrawOutcomes
	for index := 0; index < cards.DeckSize; index++ {
		card := cards.CardFromIndex(index)
		if held.Has(card) {
			continue
		}
		candidate := append(append([]cards.Card(nil), base...), card)
		value := deuce.Eval(candidate)
		out.Total++
		if value > current {
			out.Better++
		}
		switch deuce.CategoryOf(value) {
		case deuce.Seven:
			out.Seven++
		case deuce.Eight:
			out.Eight++
		case deuce.Nine:
			out.Nine++
		}
	}
	return out
}

func (o DrawOutcomes) BetterRate() float64 {
	if o.Total == 0 {
		return 0
	}
	return float64(o.Better) / float64(o.Total)
}

func cleanFourCardEight(keep []cards.Rank) bool {
	if len(keep) != 4 || keep[3] > cards.Eight {
		return false
	}
	for i := 1; i < len(keep); i++ {
		if keep[i] <= keep[i-1] {
			return false
		}
	}
	return true
}

func cleanFourCardNine(keep []cards.Rank) bool {
	if len(keep) != 4 || keep[3] > cards.Nine {
		return false
	}
	for i := 1; i < len(keep); i++ {
		if keep[i] <= keep[i-1] {
			return false
		}
	}
	return true
}

func (s *State) AllLiveDrewAtLeastCurrent(minimum int) bool {
	draws, ok := s.CurrentLiveDraws()
	if !ok || len(draws) == 0 {
		return false
	}
	for _, draw := range draws {
		if draw < minimum {
			return false
		}
	}
	return true
}

func (s *State) LiveHadPriorPat() bool {
	for seat := range s.Hand.Seats {
		if seat == s.Hand.Hero || s.Hand.Seats[seat].Folded {
			continue
		}
		for street := Draw1; street < s.Hand.Street && street <= Draw3; street++ {
			if s.Hand.Seats[seat].Draws[street] == 0 {
				return true
			}
		}
	}
	return false
}

func drawingDiscards(hand []cards.Card) []cards.Card {
	return policy.Discards(hand, policy.DrawingKeep(hand))
}

func exactNuts(hand []cards.Card) bool {
	if len(hand) != deuce.HandSize {
		return false
	}
	return deuce.Eval(hand) == deuce.Eval(cards.MustParse("2c", "3d", "4h", "5s", "7c"))
}

func smoothEight(hand []cards.Card) bool { return smoothMade(hand, deuce.Eight, cards.Six) }
func smoothNine(hand []cards.Card) bool  { return smoothMade(hand, deuce.Nine, cards.Six) }
func riverSmoothNine(hand []cards.Card) bool {
	return smoothMade(hand, deuce.Nine, cards.Seven)
}

var strongSevenFloor = deuce.Eval(cards.MustParse("2c", "3d", "5h", "6s", "7c"))

func smoothMade(hand []cards.Card, category deuce.Category, secondHigh cards.Rank) bool {
	if deuce.Categorize(hand) != category {
		return false
	}
	ranks := cards.DistinctRanks(hand)
	return len(ranks) == deuce.HandSize && ranks[len(ranks)-2] <= secondHigh
}

func (s *State) LivePatPressure() (fresh, stale int) {
	for _, read := range s.LiveOpponentDraws() {
		if !read.Known || read.Count != 0 {
			continue
		}
		if read.StreetsAgo == 0 {
			fresh++
		} else {
			stale++
		}
	}
	return fresh, stale
}

func (s *State) AllLiveDrawingCurrent() bool {
	draws, ok := s.CurrentLiveDraws()
	if !ok || len(draws) == 0 {
		return false
	}
	for _, draw := range draws {
		if draw == 0 {
			return false
		}
	}
	return true
}
