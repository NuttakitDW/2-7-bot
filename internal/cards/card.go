// Package cards is the wire protocol's card vocabulary: a rank, a suit, and
// the two-character text form every message uses.
//
// The ace is always high in 2-7 and never completes a wheel
// (docs/game/rules.md), so nothing here needs an ace-low special case.
// Two is the best card in the game; Ace is the worst.
package cards

import (
	"encoding/json"
	"fmt"
)

// Rank is a card's rank, numbered naturally so that a lower Rank is a better
// card for lowball.
type Rank uint8

// The thirteen ranks.
const (
	Two Rank = iota + 2
	Three
	Four
	Five
	Six
	Seven
	Eight
	Nine
	Ten
	Jack
	Queen
	King
	Ace
)

// rankLetters is the wire's rank alphabet, indexed by Rank-Two.
const rankLetters = "23456789TJQKA"

// Index is the engine's rank encoding — Two = 0 … Ace = 12 — which every
// tiebreak nibble of a HandValue is written in (eval/mod.rs:11-20).
func (r Rank) Index() uint32 { return uint32(r) - uint32(Two) }

func (r Rank) String() string {
	if r < Two || r > Ace {
		return "?"
	}
	return string(rankLetters[r-Two])
}

// Suit is a card's suit. Suits are irrelevant to 2-7 except for the flush
// check itself (docs/game/rules.md), so they are never ordered.
type Suit uint8

// The four suits, valued as the wire characters they are written with.
const (
	Clubs    Suit = 'c'
	Diamonds Suit = 'd'
	Hearts   Suit = 'h'
	Spades   Suit = 's'
)

func (s Suit) String() string { return string(rune(s)) }

// Card is one playing card.
type Card struct {
	Rank Rank
	Suit Suit
}

// String renders the wire form: rank then suit, e.g. "As", "Td", "2c"
// (WIRE_PROTOCOL.md:197-198). Rank is upper case, suit lower.
func (c Card) String() string { return c.Rank.String() + c.Suit.String() }

// ParseCard reads the wire form. It accepts either case on both characters —
// the protocol only ever emits "As" style, but being lenient on input costs
// nothing and a mis-cased card would otherwise become a fault.
func ParseCard(text string) (Card, error) {
	if len(text) != 2 {
		return Card{}, fmt.Errorf("card %q: want exactly two characters", text)
	}
	rank, ok := rankFrom(text[0])
	if !ok {
		return Card{}, fmt.Errorf("card %q: unknown rank %q", text, text[:1])
	}
	suit, ok := suitFrom(text[1])
	if !ok {
		return Card{}, fmt.Errorf("card %q: unknown suit %q", text, text[1:])
	}
	return Card{Rank: rank, Suit: suit}, nil
}

func rankFrom(char byte) (Rank, bool) {
	if char >= 'a' && char <= 'z' {
		char -= 'a' - 'A'
	}
	for i := 0; i < len(rankLetters); i++ {
		if rankLetters[i] == char {
			return Two + Rank(i), true
		}
	}
	return 0, false
}

func suitFrom(char byte) (Suit, bool) {
	if char >= 'A' && char <= 'Z' {
		char += 'a' - 'A'
	}
	switch Suit(char) {
	case Clubs, Diamonds, Hearts, Spades:
		return Suit(char), true
	}
	return 0, false
}

// MarshalJSON writes a Card as the wire's two-character string.
func (c Card) MarshalJSON() ([]byte, error) { return json.Marshal(c.String()) }

// UnmarshalJSON reads a Card from the wire's two-character string.
func (c *Card) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	parsed, err := ParseCard(text)
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}
