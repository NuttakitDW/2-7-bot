package policy

import "github.com/nuttakit/2-7-bot/internal/table"

// The draw table's context axis: which street this draw is, crossed with a
// bucketed read. The bucket keeps the two facts the old rule threw away —
// staleness and the street — while collapsing counts above two, which say
// nothing a two does not.
//
// Position needs no axis of its own: heads-up only the big blind ever sees
// a blind or stale read, and only the button a fresh one, so the bucket
// carries it.
const (
	readNone = iota // no opponent draw seen yet — the big blind's first draw
	readFreshPat
	readFreshOne
	readFreshTwo // drew two or more, seen this street
	readStalePat
	readStaleOne
	readStaleTwo
	readBuckets
)

// NumDrawContexts is the width of the generated draw table's context axis.
const NumDrawContexts = (table.Draw3 - table.Draw1 + 1) * readBuckets

// drawNoData marks a draw-table cell the generator could not decide — too
// few samples reached it — which defers to the structural rule. Shared with
// cmd/drawgen's emitter by value, like the chartTable bits.
const drawNoData = 0xFF

// DrawContext flattens a street and a read into a draw-table column.
func DrawContext(street int, read Read) int {
	return (street-table.Draw1)*readBuckets + readBucket(read)
}

func readBucket(read Read) int {
	if !read.Known {
		return readNone
	}
	bucket := readFreshPat
	switch {
	case read.Count == 1:
		bucket = readFreshOne
	case read.Count >= 2:
		bucket = readFreshTwo
	}
	if read.StreetsAgo > 0 {
		bucket += readStalePat - readFreshPat
	}
	return bucket
}
