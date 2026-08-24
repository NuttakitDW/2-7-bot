// Package deuce evaluates five-card 2-7 (deuce-to-seven) lowball hands in the
// engine's own frozen encoding.
//
// The whole evaluator is one identity, from eval/low.rs:16-24:
//
//	deuce_to_seven(hand) = 0x00FF_FFFF - rank_five_no_wheel(hand)
//
// where rank_five_no_wheel is the ordinary five-card *high* classifier with
// the ace-low wheel exception switched off (eval/high.rs:78-85). So straights
// and flushes count against you, the ace is always high, and A-5-4-3-2 is an
// ace-high no-pair hand rather than a straight.
//
// Two simplifications relative to a hold'em evaluator: 27td-fl hands are
// exactly five cards with HoleUsage::AllOwn, so there is no best-subset
// search; and suits matter only for the flush check, never for a tiebreak.
//
// Reproducing the engine's encoding exactly — rather than merely its ordering
// — is deliberate. It makes every showdown-show.hi value on the wire directly
// comparable against a locally computed one, which turns any spar log into a
// correctness oracle. See eval_test.go.
package deuce

import "github.com/nuttakit/2-7-bot/internal/cards"

// HandSize is the number of cards in a 27td-fl hand (game/spec.rs:651).
const HandSize = 5

// Value is a hand's strength. Greater is better, as everywhere in the
// engine, and values are only comparable against other Values.
type Value uint32

// invert is the constant 2-7 subtracts the high encoding from
// (eval/mod.rs:28-30).
const invert = 0x00FF_FFFF

// Class is the ordinary high-hand class occupying bits [20..24) of a high
// encoding (poker-wire/src/value.rs:12-22). It is named here because the 2-7
// ordering is the high ordering reversed, so "which class" still explains
// why one hand beats another.
type Class uint32

// The nine high-hand classes, in the engine's discriminant order. For 2-7 the
// ordering is inverted: HighCard is the only class that can win.
const (
	HighCard Class = iota
	OnePair
	TwoPair
	Trips
	Straight
	Flush
	FullHouse
	Quads
	StraightFlush
)

var className = [...]string{
	"high card", "one pair", "two pair", "trips", "straight",
	"flush", "full house", "quads", "straight flush",
}

func (c Class) String() string {
	if int(c) >= len(className) {
		return "?"
	}
	return className[c]
}

// Eval ranks exactly five cards. It panics on any other count, because a hand
// of the wrong size is a bug in the caller's state tracking rather than
// something the arena can send.
func Eval(hand []cards.Card) Value {
	if len(hand) != HandSize {
		panic("deuce: Eval requires exactly five cards")
	}
	return Value(invert - rankFiveNoWheel(hand))
}

// Class reports the high-hand class this value was built from. For 2-7 only
// HighCard hands are lows; everything else is a broken hand.
func (v Value) Class() Class { return Class((invert - uint32(v)) >> 20) }

// rankFiveNoWheel is eval/high.rs's classify(cards, allow_wheel: false),
// transcribed. Ranks are held in the engine's Rank::index space
// (Two = 0 … Ace = 12) throughout, because that is what the tiebreak nibbles
// are written in.
func rankFiveNoWheel(hand []cards.Card) uint32 {
	var counts [13]uint32
	for _, card := range hand {
		counts[card.Rank.Index()]++
	}

	// Ranks descending, which is the order every no-pair tiebreak wants.
	descending := make([]uint32, 0, HandSize)
	for index := 12; index >= 0; index-- {
		for repeat := uint32(0); repeat < counts[index]; repeat++ {
			descending = append(descending, uint32(index))
		}
	}

	flush := cards.SameSuit(hand)
	straightHigh, isStraight := straightHighCard(counts)

	switch {
	case isStraight && flush:
		return encode(StraightFlush, straightHigh)
	case isStraight:
		return encode(Straight, straightHigh)
	case flush:
		return encode(Flush, descending...)
	}

	// (count, rank) groups sorted so the biggest — and among equals, the
	// highest — leads. That is exactly the tiebreak order of every paired
	// class (eval/high.rs:100-107).
	groups := make([]group, 0, HandSize)
	for index := 12; index >= 0; index-- {
		if counts[index] > 0 {
			groups = append(groups, group{counts[index], uint32(index)})
		}
	}
	sortGroups(groups)

	switch {
	case groups[0].count == 4:
		return encode(Quads, groups[0].rank, groups[1].rank)
	case groups[0].count == 3 && groups[1].count == 2:
		return encode(FullHouse, groups[0].rank, groups[1].rank)
	case groups[0].count == 3:
		return encode(Trips, groups[0].rank, groups[1].rank, groups[2].rank)
	case groups[0].count == 2 && groups[1].count == 2:
		return encode(TwoPair, groups[0].rank, groups[1].rank, groups[2].rank)
	case groups[0].count == 2:
		return encode(OnePair, groups[0].rank, groups[1].rank, groups[2].rank, groups[3].rank)
	default:
		return encode(HighCard, descending...)
	}
}

// group is one rank and how many times the hand holds it.
type group struct{ count, rank uint32 }

// sortGroups orders (count, rank) pairs descending on count then rank. Five
// cards make at most five groups, so an insertion sort is both the clearest
// and the fastest thing to write here.
func sortGroups(groups []group) {
	for i := 1; i < len(groups); i++ {
		for j := i; j > 0; j-- {
			left, right := groups[j-1], groups[j]
			if left.count > right.count || (left.count == right.count && left.rank >= right.rank) {
				break
			}
			groups[j-1], groups[j] = right, left
		}
	}
}

// straightHighCard is eval/high.rs:131-147 with allow_wheel false: five
// distinct ranks spanning exactly four steps. The wheel branch is simply
// absent, which is the entire difference between high poker and 2-7.
func straightHighCard(counts [13]uint32) (uint32, bool) {
	low, high := -1, -1
	for index := 0; index < 13; index++ {
		switch {
		case counts[index] > 1:
			return 0, false // a pair is never a straight
		case counts[index] == 1:
			if low < 0 {
				low = index
			}
			high = index
		}
	}
	if high-low != 4 {
		return 0, false
	}
	return uint32(high), true
}

// encode packs a class and up to five 4-bit tiebreak ranks, most significant
// first, unused trailing slots zero (eval/high.rs:151-157).
func encode(class Class, tiebreaks ...uint32) uint32 {
	value := uint32(class) << 20
	for i, rank := range tiebreaks {
		if i >= HandSize {
			break
		}
		value |= rank << (16 - 4*i)
	}
	return value
}
