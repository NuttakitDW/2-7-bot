package deuce

import (
	"runtime"
	"sync"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

// Table is Eval precomputed for every five-card deal, indexed by
// cards.Set.DealIndex. A lookup is one array index instead of a histogram, a
// straight scan, a group sort and a variadic pack.
//
// It is derived from Eval rather than reimplementing it, so the engine's
// frozen encoding is stated in exactly one place and the oracle tests keep
// covering both. TestTableMatchesEval proves the two agree on all 2,598,960
// deals.
//
// This is deliberately *not* built by the bot. Building costs 69ms and 10 MB
// (2,598,960 × 4 bytes), and the bot makes four decisions a hand — it has no
// hot loop to speed up. The callers are the offline equity and abstraction
// builders, which evaluate billions of hands and for which Eval is the binding
// constraint: 56ns against 12ns for a lookup that misses cache every time,
// measured on an M4 Pro on 2026-08-24 by the benchmarks in this package.
type Table []Value

// NewTable builds the lookup across all available cores.
func NewTable() Table {
	table := make(Table, cards.Deals)

	workers := runtime.GOMAXPROCS(0)
	span := (cards.Deals + workers - 1) / workers

	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		start := worker * span
		end := start + span
		if end > cards.Deals {
			end = cards.Deals
		}
		if start >= end {
			break
		}
		wait.Add(1)
		go func(start, end int) {
			defer wait.Done()
			hand := make([]cards.Card, 0, HandSize)
			for index := start; index < end; index++ {
				hand = cards.DealFromIndex(index).Append(hand[:0])
				table[index] = Eval(hand)
			}
		}(start, end)
	}
	wait.Wait()

	return table
}

// Lookup is the value of the deal with this index.
func (t Table) Lookup(dealIndex int) Value { return t[dealIndex] }

// Value is the value of a set of exactly five cards.
func (t Table) Value(hand cards.Set) Value { return t[hand.DealIndex()] }
