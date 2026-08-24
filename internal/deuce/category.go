package deuce

import "github.com/nuttakit/2-7-bot/internal/cards"

// Category is the coarse hand-strength ladder the strategy reasons about:
//
//	seven low or better  almost always wins
//	eight low            usually good enough
//	nine low             genuinely marginal — wins some, loses some
//	ten low              occasionally good
//	jack low or worse    rarely good
//
// Those are **folk-knowledge priors, not measured equities**. Nothing in this
// repo computes them. The nine-low boundary is where the expensive decisions
// live, so it is the first row a later generation should replace with a number
// from exhaustive one-draw enumeration.
//
// Ordered so that a greater Category is a better hand, which lets the
// betting rules read as plain comparisons.
type Category uint8

// The ladder, worst to best.
const (
	// Broken is any pair, straight or flush — in 2-7 those are not lows at
	// all, and a nine low beats every one of them (eval/low.rs:190-195).
	Broken Category = iota
	// Weak is a jack-high or worse no-pair hand.
	Weak
	// Ten is a ten-high no-pair hand.
	Ten
	// Nine is a nine-high no-pair hand.
	Nine
	// Eight is an eight-high no-pair hand.
	Eight
	// Seven is a seven-high no-pair hand — the best category there is,
	// topped by the nut hand 7-5-4-3-2 (eval/low.rs:176). A six-high
	// no-pair hand cannot exist: 6-5-4-3-2 is a straight.
	Seven
)

var categoryName = [...]string{"broken", "weak", "ten", "nine", "eight", "seven"}

func (c Category) String() string {
	if int(c) >= len(categoryName) {
		return "?"
	}
	return categoryName[c]
}

// HighCard is the highest rank of a no-pair hand. It is meaningful only when
// Class is HighCard; anything else reports the leading tiebreak nibble, which
// is a pair or straight rank instead.
func (v Value) HighCard() cards.Rank {
	high := uint32(invert) - uint32(v)
	return cards.Two + cards.Rank((high>>16)&0xF)
}

// CategoryOf places a value on the ladder.
func CategoryOf(value Value) Category {
	if value.Class() != HighCard {
		return Broken
	}
	switch value.HighCard() {
	case cards.Seven:
		return Seven
	case cards.Eight:
		return Eight
	case cards.Nine:
		return Nine
	case cards.Ten:
		return Ten
	default:
		return Weak
	}
}

// Categorize evaluates a five-card hand straight onto the ladder.
func Categorize(hand []cards.Card) Category { return CategoryOf(Eval(hand)) }
