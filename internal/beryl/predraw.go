package beryl

import (
	"sort"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
)

type rangeAction uint8

const (
	rangeFold rangeAction = iota
	rangeCall
	rangeRaise
)

// Range source: https://www.nuttakitkundum.com/triple-draw-hands (displayed
// Deuce-to-Seven chart). The chart distinguishes no open-ender from no
// straight draw: ace is high, so 2345 has only one straight completion.
func openingPlayable(hand []cards.Card, pos Position) bool {
	if len(hand) != 5 || cards.NewSet(hand).Len() != 5 {
		return false
	}
	if pat96(hand) || (pos == Button || pos == SmallBlind) && patNine(hand) {
		return true
	}
	ranks := uniqueRanks(hand)
	switch pos {
	case UnderTheGun:
		return anySubset(ranks, 4, utgFour) || anySubset(ranks, 3, utgThree)
	case Hijack:
		return anySubset(ranks, 4, hijackFour) || anySubset(ranks, 3, hijackThree)
	case Cutoff:
		return anySubset(ranks, 4, hijackFour) || anySubset(ranks, 3, cutoffThree) || anySubset(ranks, 2, cutoffTwo)
	case Button, SmallBlind:
		return anySubset(ranks, 4, hijackFour) || anySubset(ranks, 3, buttonThree) || anySubset(ranks, 2, buttonTwo)
	default:
		return anySubset(ranks, 4, hijackFour) || anySubset(ranks, 3, buttonThree) || anySubset(ranks, 2, buttonTwo)
	}
}

func facingOpenAction(hand []cards.Card, hero, opener Position) rangeAction {
	if pat96(hand) || anySubset(uniqueRanks(hand), 4, eightNoStraightDraw) {
		return rangeRaise
	}
	ranks := uniqueRanks(hand)
	if (opener == Button || opener == SmallBlind) && anySubset(ranks, 3, threeEightWithDeuce) {
		return rangeRaise
	}
	if hero == SmallBlind && (opener == Button || opener == SmallBlind) {
		return rangeFold
	}
	if (opener == UnderTheGun || opener == Hijack) && anySubset(ranks, 3, threeSevenWithDeuce) {
		return rangeCall
	}
	if opener == Cutoff && anySubset(ranks, 3, threeEightWithDeuce) {
		return rangeCall
	}
	if hero == BigBlind && (opener == Button || opener == SmallBlind) && anySubset(ranks, 2, buttonTwo) {
		return rangeCall
	}
	return rangeFold
}

func patNine(hand []cards.Card) bool { return deuce.Categorize(hand) >= deuce.Nine }

func pat96(hand []cards.Card) bool {
	category := deuce.Categorize(hand)
	if category >= deuce.Eight {
		return true
	}
	if category != deuce.Nine {
		return false
	}
	ranks := uniqueRanks(hand)
	return len(ranks) == 5 && ranks[3] <= cards.Six
}

func utgFour(r []cards.Rank) bool {
	if r[3] <= cards.Seven && containsRank(r, cards.Two) {
		return true
	}
	// “8-6-5-3+” is interpreted lexicographically while excluding real
	// two-ended runs (3456/4567); 2345 remains allowed because ace is high.
	return lexAtMost(r, []cards.Rank{cards.Three, cards.Five, cards.Six, cards.Eight}) && !openEnded(r)
}

func hijackFour(r []cards.Rank) bool {
	return r[3] <= cards.Seven && !openEnded(r) || r[3] <= cards.Eight && !straightDraw(r)
}

func eightNoStraightDraw(r []cards.Rank) bool { return r[3] <= cards.Eight && !straightDraw(r) }

func utgThree(r []cards.Rank) bool {
	return r[2] <= cards.Seven && containsRank(r, cards.Two) && containsRank(r, cards.Seven) || ranksEqual(r, cards.Two, cards.Three, cards.Four) || ranksEqual(r, cards.Two, cards.Three, cards.Eight)
}

func hijackThree(r []cards.Rank) bool {
	if utgThree(r) || ranksEqual(r, cards.Two, cards.Three, cards.Five) || ranksEqual(r, cards.Two, cards.Four, cards.Five) {
		return true
	}
	return containsRank(r, cards.Two) && (containsRank(r, cards.Six) && r[2] == cards.Six || containsRank(r, cards.Eight) && r[2] == cards.Eight)
}

func cutoffThree(r []cards.Rank) bool {
	return hijackThree(r) || r[2] <= cards.Eight && !threeConsecutive(r)
}

func buttonThree(r []cards.Rank) bool {
	return cutoffThree(r) || ranksEqual(r, cards.Three, cards.Four, cards.Five) || ranksEqual(r, cards.Three, cards.Four, cards.Six) || ranksEqual(r, cards.Three, cards.Five, cards.Six) || ranksEqual(r, cards.Three, cards.Four, cards.Seven)
}

func cutoffTwo(r []cards.Rank) bool {
	return ranksEqual(r, cards.Two, cards.Three) || ranksEqual(r, cards.Two, cards.Seven)
}
func buttonTwo(r []cards.Rank) bool {
	return r[0] == cards.Two && r[1] >= cards.Three && r[1] <= cards.Seven
}
func threeSevenWithDeuce(r []cards.Rank) bool {
	return r[2] <= cards.Seven && containsRank(r, cards.Two)
}
func threeEightWithDeuce(r []cards.Rank) bool {
	return r[2] <= cards.Eight && containsRank(r, cards.Two)
}

func uniqueRanks(hand []cards.Card) []cards.Rank {
	seen := map[cards.Rank]bool{}
	for _, card := range hand {
		seen[card.Rank] = true
	}
	out := make([]cards.Rank, 0, len(seen))
	for rank := range seen {
		out = append(out, rank)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func anySubset(ranks []cards.Rank, size int, accept func([]cards.Rank) bool) bool {
	if len(ranks) < size {
		return false
	}
	var selected []cards.Rank
	var search func(int) bool
	search = func(start int) bool {
		if len(selected) == size {
			return accept(selected)
		}
		for i := start; i <= len(ranks)-(size-len(selected)); i++ {
			selected = append(selected, ranks[i])
			if search(i + 1) {
				return true
			}
			selected = selected[:len(selected)-1]
		}
		return false
	}
	return search(0)
}

func openEnded(r []cards.Rank) bool        { return r[0] > cards.Two && r[3]-r[0] == 3 }
func straightDraw(r []cards.Rank) bool     { return r[3]-r[0] <= 4 }
func threeConsecutive(r []cards.Rank) bool { return r[2]-r[0] == 2 }

func lexAtMost(a, b []cards.Rank) bool {
	for i := len(a) - 1; i >= 0; i-- {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return true
}

func containsRank(r []cards.Rank, rank cards.Rank) bool {
	for _, candidate := range r {
		if candidate == rank {
			return true
		}
	}
	return false
}

func ranksEqual(got []cards.Rank, want ...cards.Rank) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
