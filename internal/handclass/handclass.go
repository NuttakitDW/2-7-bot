// Package handclass numbers the predraw hand classes of 2-7 triple draw.
//
// Predraw, suits carry exactly one bit of information — whether the five
// cards are a flush — because the evaluator reads suits only for the flush
// check and every draw destroys the suit pattern anyway. So a class is a
// five-rank multiset plus, where the ranks are all distinct, a flush
// variant: C(17,5) = 6,188 multisets and C(13,5) = 1,287 flush sets, 7,475
// classes over the 2,598,960 deals.
//
// The numbering is the colexicographic combination index, the same scheme
// cards.Set.DealIndex uses, so the mapping is stable by construction rather
// than by convention.
package handclass

import "github.com/nuttakit/2-7-bot/internal/cards"

// ID numbers one class, in [0, Num).
type ID uint16

const (
	// multisets is C(17,5): the non-flush classes, one per rank multiset.
	multisets = 6188
	// flushSets is C(13,5): the flush classes, one per distinct-rank set.
	flushSets = 1287
	// Num is the total number of classes.
	Num = multisets + flushSets
)

// handSize is fixed by the game; duplicated from deuce.HandSize because this
// package must stay below the evaluator in the dependency order.
const handSize = 5

// binomial holds C(n,k) for the colex arithmetic. The transform below maps
// rank multisets into strictly increasing 5-sets over 0..16, so 17 rows and
// handSize columns cover everything.
var binomial [17 + 1][handSize + 1]int

func init() {
	for n := range binomial {
		binomial[n][0] = 1
		for k := 1; k <= handSize && k <= n; k++ {
			binomial[n][k] = binomial[n-1][k-1] + binomial[n-1][k]
		}
	}
}

// Of maps five cards to their class. It reads only ranks and the flush bit,
// so any two deals with the same rank multiset and the same flushness — the
// only predraw facts the game ever scores — share an ID.
func Of(hand []cards.Card) ID {
	if len(hand) != handSize {
		panic("handclass: Of requires exactly five cards")
	}
	var ranks [handSize]int
	for i, card := range hand {
		ranks[i] = int(card.Rank.Index())
	}
	sortRanks(&ranks)

	if cards.SameSuit(hand) {
		// A real deck holds one card per rank per suit, so a flush has
		// five distinct ranks and the plain combination index applies.
		return ID(multisets + combIndex(ranks))
	}
	// The standard multiset-to-set transform: adding the position turns a
	// non-decreasing sequence over 0..12 into a strictly increasing one
	// over 0..16, bijectively.
	for i := range ranks {
		ranks[i] += i
	}
	return ID(combIndex(ranks))
}

// Representative is a concrete five-card deal in the class — the hand the
// equity rollouts simulate for everyone the class stands for.
//
// The multiset space includes the thirteen five-of-a-kind classes no real
// deck can deal; those have Weight 0 and no representative. Callers walking
// all IDs must skip zero-weight classes.
func Representative(id ID) []cards.Card {
	if id >= Num {
		panic("handclass: Representative of an ID out of range")
	}
	if Weight(id) == 0 {
		panic("handclass: Representative of an impossible class")
	}
	if int(id) >= multisets {
		ranks := unrank(int(id) - multisets)
		hand := make([]cards.Card, handSize)
		for i, rank := range ranks {
			hand[i] = cards.Card{Rank: cards.Two + cards.Rank(rank), Suit: cards.Clubs}
		}
		return hand
	}
	ranks := unrank(int(id))
	hand := make([]cards.Card, handSize)
	for i, rank := range ranks {
		// Undo the multiset transform, then cycle suits by position: five
		// positions over four suits can never be uniform, and duplicate
		// ranks sit in adjacent positions so they never collide either.
		hand[i] = cards.Card{Rank: cards.Two + cards.Rank(rank-i), Suit: suitCycle[i%len(suitCycle)]}
	}
	return hand
}

var suitCycle = [4]cards.Suit{cards.Clubs, cards.Diamonds, cards.Hearts, cards.Spades}

// Weight is how many of the 2,598,960 deals the class stands for. Range
// frequencies must be computed in deal weight, not class count — a class
// with a pair covers a very different slice of the deck than one without.
func Weight(id ID) int {
	if int(id) >= multisets {
		return 4 // one flush per suit
	}
	var counts [13]int
	for i, rank := range unrank(int(id)) {
		counts[rank-i]++
	}
	weight, distinct := 1, 0
	for _, count := range counts {
		if count > 0 {
			weight *= binomial4[count]
			distinct++
		}
	}
	if distinct == handSize {
		return weight - 4 // the four flushes are their own classes
	}
	return weight
}

// binomial4 is C(4,k): the ways to pick suits for k copies of a rank.
var binomial4 = [handSize + 1]int{1, 4, 6, 4, 1}

// combIndex is the colex rank of a strictly increasing 5-set.
func combIndex(ascending [handSize]int) int {
	index := 0
	for i, value := range ascending {
		index += binomial[value][i+1]
	}
	return index
}

// unrank inverts combIndex.
func unrank(index int) [handSize]int {
	var out [handSize]int
	for position := handSize; position >= 1; position-- {
		value := position - 1
		for binomial[value+1][position] <= index {
			value++
		}
		out[position-1] = value
		index -= binomial[value][position]
	}
	return out
}

// sortRanks is an insertion sort over the five ranks — the input is tiny and
// this keeps Of allocation-free for the per-decision hot path.
func sortRanks(ranks *[handSize]int) {
	for i := 1; i < handSize; i++ {
		for j := i; j > 0 && ranks[j] < ranks[j-1]; j-- {
			ranks[j], ranks[j-1] = ranks[j-1], ranks[j]
		}
	}
}
