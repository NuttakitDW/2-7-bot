package cards

import "math/bits"

// DeckSize is the standard deck (deck.rs:20). 27td-fl uses no jokers and no
// stripped ranks.
const DeckSize = 52

// dealSize is the number of cards in a 27td-fl hand. It duplicates
// deuce.HandSize rather than importing it, because cards must not depend on
// the evaluator — the dependency runs the other way.
const dealSize = 5

// Deals is the number of distinct five-card deals, C(52,5). Every deal has a
// unique index in [0, Deals), which is what the abstraction's bucket tables
// are keyed on.
const Deals = 2598960

// suits fixes the suit half of a card's index. Suits are unordered in 2-7 —
// this exists to make the index a bijection, not to rank anything.
var suits = [4]Suit{Clubs, Diamonds, Hearts, Spades}

// Index is a card's position in the canonical 0..51 ordering: rank-major, so
// consecutive indices share a rank and ascending index means descending
// lowball quality. It panics on a malformed card, which can only be a bug in
// the caller — every card off the wire came through ParseCard.
func (c Card) Index() int {
	if c.Rank < Two || c.Rank > Ace {
		panic("cards: Index of a card with no rank")
	}
	for i, suit := range suits {
		if suit == c.Suit {
			return int(c.Rank.Index())*4 + i
		}
	}
	panic("cards: Index of a card with no suit")
}

// CardFromIndex inverts Card.Index.
func CardFromIndex(index int) Card {
	if index < 0 || index >= DeckSize {
		panic("cards: card index out of range")
	}
	return Card{Rank: Two + Rank(index/4), Suit: suits[index%4]}
}

// Set is a set of cards as a 52-bit mask, one bit per Card.Index. It is a
// value type, so every operation returns a new Set and none mutate the
// receiver.
//
// This is the representation the offline table builders use. The wire path
// keeps []Card: it is clearer, and four decisions a hand is not a hot loop.
type Set uint64

// NewSet collects a hand into a Set. Duplicate cards collapse, so the result's
// Len is the caller's check that the input was well formed.
func NewSet(hand []Card) Set {
	var set Set
	for _, card := range hand {
		set |= 1 << uint(card.Index())
	}
	return set
}

// Add returns the set with card added.
func (s Set) Add(card Card) Set { return s | 1<<uint(card.Index()) }

// Remove returns the set with card removed.
func (s Set) Remove(card Card) Set { return s &^ (1 << uint(card.Index())) }

// Has reports whether the set holds card.
func (s Set) Has(card Card) bool { return s&(1<<uint(card.Index())) != 0 }

// Len is the number of cards in the set.
func (s Set) Len() int { return bits.OnesCount64(uint64(s)) }

// Complement is every card the set does not hold — the rest of the deck.
func (s Set) Complement() Set { return ^s & (1<<DeckSize - 1) }

// Append adds the set's cards to dst in ascending index order and returns the
// extended slice. Passing a reused buffer keeps the enumeration loops
// allocation-free.
func (s Set) Append(dst []Card) []Card {
	for rest := uint64(s); rest != 0; rest &= rest - 1 {
		dst = append(dst, CardFromIndex(bits.TrailingZeros64(rest)))
	}
	return dst
}

// binomial holds C(n,k) for the ranks the colex index needs.
var binomial [DeckSize + 1][dealSize + 1]int

func init() {
	for n := 0; n <= DeckSize; n++ {
		binomial[n][0] = 1
		for k := 1; k <= dealSize && k <= n; k++ {
			binomial[n][k] = binomial[n-1][k-1] + binomial[n-1][k]
		}
	}
}

// DealIndex is the colexicographic index of a five-card set, a bijection onto
// [0, Deals).
//
// Colex rather than lex because its rank formula is a plain sum of binomials
// over the set's own elements — no reference to the deck size — so the same
// index survives if the enumeration is ever split or reordered.
func (s Set) DealIndex() int {
	if s.Len() != dealSize {
		panic("cards: DealIndex requires exactly five cards")
	}
	index, position := 0, 1
	for rest := uint64(s); rest != 0; rest &= rest - 1 {
		index += binomial[bits.TrailingZeros64(rest)][position]
		position++
	}
	return index
}

// DealFromIndex inverts Set.DealIndex.
func DealFromIndex(index int) Set {
	if index < 0 || index >= Deals {
		panic("cards: deal index out of range")
	}
	var set Set
	for position := dealSize; position >= 1; position-- {
		card := position - 1
		for binomial[card+1][position] <= index {
			card++
		}
		set |= 1 << uint(card)
		index -= binomial[card][position]
	}
	return set
}
