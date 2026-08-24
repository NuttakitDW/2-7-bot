package cards

import "sort"

// Helpers over a hand of cards. Every function returns a new slice rather
// than reordering its argument in place: a hand travels from the event
// decoder into the strategy and back out as a discard list, and a hidden
// mutation anywhere along that path is a fault waiting to happen.

// SortedByRank returns a copy ordered from best lowball card to worst —
// deuce first, ace last. Ties between suits keep a stable, deterministic
// order so two equal hands always produce the same discard list.
func SortedByRank(hand []Card) []Card {
	sorted := make([]Card, len(hand))
	copy(sorted, hand)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Rank != sorted[j].Rank {
			return sorted[i].Rank < sorted[j].Rank
		}
		return sorted[i].Suit < sorted[j].Suit
	})
	return sorted
}

// DistinctRanks returns the hand's ranks, deduplicated, ascending.
func DistinctRanks(hand []Card) []Rank {
	sorted := SortedByRank(hand)
	ranks := make([]Rank, 0, len(sorted))
	for _, card := range sorted {
		if len(ranks) == 0 || ranks[len(ranks)-1] != card.Rank {
			ranks = append(ranks, card.Rank)
		}
	}
	return ranks
}

// SameSuit reports whether every card shares one suit — the only thing suits
// are ever asked in this game.
func SameSuit(hand []Card) bool {
	if len(hand) == 0 {
		return false
	}
	for _, card := range hand[1:] {
		if card.Suit != hand[0].Suit {
			return false
		}
	}
	return true
}

// Contains reports whether the hand holds this exact card.
func Contains(hand []Card, card Card) bool {
	for _, held := range hand {
		if held == card {
			return true
		}
	}
	return false
}

// Without returns a copy of hand with one copy of each card in drop removed.
// Cards in drop that the hand does not hold are ignored, which is what makes
// this safe to point at a draw-result echoed back by the arena.
func Without(hand, drop []Card) []Card {
	remaining := make([]Card, 0, len(hand))
	dropped := make([]Card, len(drop))
	copy(dropped, drop)
	for _, card := range hand {
		if index := indexOf(dropped, card); index >= 0 {
			dropped = append(dropped[:index], dropped[index+1:]...)
			continue
		}
		remaining = append(remaining, card)
	}
	return remaining
}

// With returns a copy of hand with extra appended.
func With(hand, extra []Card) []Card {
	joined := make([]Card, 0, len(hand)+len(extra))
	joined = append(joined, hand...)
	joined = append(joined, extra...)
	return joined
}

// Strings renders a hand in wire form, for logging and for tests.
func Strings(hand []Card) []string {
	out := make([]string, len(hand))
	for i, card := range hand {
		out[i] = card.String()
	}
	return out
}

// Parse reads a slice of wire-form cards.
func Parse(texts []string) ([]Card, error) {
	hand := make([]Card, len(texts))
	for i, text := range texts {
		card, err := ParseCard(text)
		if err != nil {
			return nil, err
		}
		hand[i] = card
	}
	return hand, nil
}

// MustParse is Parse for test tables and compiled-in constants, where a
// malformed card is a programming error rather than input.
func MustParse(texts ...string) []Card {
	hand, err := Parse(texts)
	if err != nil {
		panic(err)
	}
	return hand
}

func indexOf(hand []Card, card Card) int {
	for i, held := range hand {
		if held == card {
			return i
		}
	}
	return -1
}
