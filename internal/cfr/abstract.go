package cfr

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/policy"
)

// The card abstraction. Every private fact the strategy reads is a function
// of the hand's class (handclass.Of: rank multiset plus flushness), so the
// whole abstraction is a table over the 7,475 classes, built once.
//
// Three views of a hand, coarse to fine:
//
//   - Predraw betting keys on the exact class — lossless, and cheap at
//     eight nodes.
//   - Betting with draws still to come keys on a shape bucket: a made hand
//     by its top two cards, a drawing hand by how many cards the structural
//     rule keeps and how low they are.
//   - Betting after the last draw keys on a showdown bucket, finer at the
//     top where the money is.
//   - Draw decisions key on the distinct-rank set plus flushness (a pair's
//     second copy is always thrown, except as a snow, which every class
//     can express through the stand-pat candidate), with the candidate
//     list from policy.DrawCandidates as the action set. That list is what
//     the runtime reconstructs, so an index into it is a portable action.

const (
	NumDrawBuckets  = 21
	NumFinalBuckets = 18
	MaxCand         = policy.MaxDrawCandidates
)

// ClassInfo is the abstraction's reading of one hand class.
type ClassInfo struct {
	NumCand uint8
	// Keep[i] is candidate i as a bitmask over the hand sorted by rank
	// (deuce first, suits ascending within a rank): bit p set keeps card p.
	Keep      [MaxCand]uint8
	Draw      uint8
	Final     uint8
	DrawClass uint16
}

// Abstraction is the per-class table plus the size of the draw-class space.
type Abstraction struct {
	Classes        []ClassInfo
	NumDrawClasses int
}

// BuildAbstraction computes the table.
func BuildAbstraction() *Abstraction {
	a := &Abstraction{Classes: make([]ClassInfo, handclass.Num)}
	drawClasses := map[uint16]uint16{}
	for id := 0; id < handclass.Num; id++ {
		if handclass.Weight(handclass.ID(id)) == 0 {
			continue
		}
		hand := cards.SortedByRank(handclass.Representative(handclass.ID(id)))
		info := &a.Classes[id]
		for i, keep := range policy.DrawCandidates(hand) {
			info.Keep[i] = keepMask(hand, keep)
			info.NumCand++
		}
		info.Draw = drawBucket(hand)
		info.Final = finalBucket(hand)

		key := uint16(0)
		for _, rank := range cards.DistinctRanks(hand) {
			key |= 1 << rank.Index()
		}
		if cards.SameSuit(hand) {
			key |= 1 << 13
		}
		if _, ok := drawClasses[key]; !ok {
			drawClasses[key] = uint16(len(drawClasses))
		}
		info.DrawClass = drawClasses[key]
	}
	a.NumDrawClasses = len(drawClasses)
	return a
}

// keepMask encodes a keep list the way policy.Discards reads it: one card
// per listed rank, the first copy in sorted order, and every copy for the
// stand pat, which lists duplicates.
func keepMask(sorted []cards.Card, keep []cards.Rank) uint8 {
	wanted := map[cards.Rank]int{}
	for _, rank := range keep {
		wanted[rank]++
	}
	var mask uint8
	for i, card := range sorted {
		if wanted[card.Rank] > 0 {
			wanted[card.Rank]--
			mask |= 1 << i
		}
	}
	return mask
}

// Class is the handclass of a sorted five-card hand.
func Class(hand []cards.Card) int { return int(handclass.Of(hand)) }

func drawBucket(hand []cards.Card) uint8 {
	distinct := cards.DistinctRanks(hand)
	if deuce.Eval(hand).Class() == deuce.HighCard && distinct[4] <= cards.Ten {
		second := distinct[3]
		switch distinct[4] {
		case cards.Seven:
			return pick(second <= cards.Five, 0, 1)
		case cards.Eight:
			return ladder(second, []cards.Rank{cards.Five, cards.Six}, 2)
		case cards.Nine:
			return ladder(second, []cards.Rank{cards.Six, cards.Seven}, 5)
		default:
			return pick(second <= cards.Seven, 8, 9)
		}
	}
	keep := policy.DrawingKeep(hand)
	switch len(keep) {
	case 4:
		return ladder(keep[3], []cards.Rank{cards.Seven, cards.Eight}, 10)
	case 3:
		return ladder(keep[2], []cards.Rank{cards.Five, cards.Six, cards.Seven, cards.Eight}, 13)
	case 2:
		return pick(keep[0] == cards.Two, 18, 19)
	default:
		return 20
	}
}

func finalBucket(hand []cards.Card) uint8 {
	value := deuce.Eval(hand)
	if value.Class() != deuce.HighCard {
		return pick(value.Class() == deuce.OnePair, 16, 17)
	}
	distinct := cards.DistinctRanks(hand)
	top, second, third := distinct[4], distinct[3], distinct[2]
	switch top {
	case cards.Seven:
		switch {
		case second == cards.Five && third == cards.Four:
			return 0
		case second == cards.Five:
			return 1
		default:
			return 2
		}
	case cards.Eight:
		switch {
		case second <= cards.Five:
			return 3
		case second == cards.Six:
			return 4
		default:
			return pick(third <= cards.Five, 5, 6)
		}
	case cards.Nine:
		switch {
		case second <= cards.Six:
			return 7
		case second == cards.Seven:
			return 8
		default:
			return pick(third <= cards.Five, 9, 10)
		}
	case cards.Ten:
		return pick(second <= cards.Seven, 11, 12)
	case cards.Jack:
		return 13
	case cards.Queen:
		return 14
	default:
		return 15
	}
}

// ladder buckets a rank against ascending thresholds: base for at or
// under the first, base+1 for at or under the second, and so on, with one
// more bucket past the last threshold.
func ladder(rank cards.Rank, thresholds []cards.Rank, base uint8) uint8 {
	for i, threshold := range thresholds {
		if rank <= threshold {
			return base + uint8(i)
		}
	}
	return base + uint8(len(thresholds))
}

func pick(cond bool, yes, no uint8) uint8 {
	if cond {
		return yes
	}
	return no
}
