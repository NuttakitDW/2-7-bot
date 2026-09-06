package cfr

import "github.com/nuttakit/2-7-bot/internal/handclass"

// Layout maps an information set to a slot in the flat strategy tables.
// Trainer and Blueprint share it, so it is a pure function of the tree
// and the abstraction.
//
// Betting sets are (node, context, bucket): the node carries the whole
// betting history of the hand, the context the draw counts that matter
// most — this street's for both players and the opponent's previous one —
// and the bucket the hand.
//
// Draw sets are coarser on the public side, because the draw is where the
// exact hand matters most and the table cannot afford both: (street,
// drawer, who bet last, context, draw class).
type Layout struct {
	BetSlots  int64
	DrawSlots int64
	// drawClasses is the draw-class count the draw index arithmetic uses.
	drawClasses  int64
	finalBuckets int
	drawBuckets  int
}

// Context sizes: draw counts clip at three, four values per reading.
const (
	drawClip    = 3
	drawReads   = drawClip + 1
	betCtxDraw1 = drawReads * drawReads
	betCtxLate  = drawReads * drawReads * drawReads
	drawCtx     = drawReads * drawReads
	aggrStates  = 3 // nobody, the drawer, the opponent bet last
)

// layoutProfile is fixed at build time. Compact layouts deliberately forget
// earlier betting order when the current contributions and action state match.
var layoutProfile = "history"

// NewLayout assigns every betting node its offset and sizes the tables.
func NewLayout(t *Tree, a *Abstraction) *Layout {
	switch layoutProfile {
	case "history", "history-rich":
		return newHistoryLayout(t, a)
	case "compact", "compact-rich":
		return newCompactLayout(t, a)
	default:
		panic("unknown CFR layout profile: " + layoutProfile)
	}
}

func newHistoryLayout(t *Tree, a *Abstraction) *Layout {
	l := &Layout{drawClasses: int64(a.NumDrawClasses), finalBuckets: a.FinalBuckets, drawBuckets: a.DrawBuckets}
	var offset int64
	for i := range t.Nodes {
		node := &t.Nodes[i]
		if node.Kind != KindBet {
			continue
		}
		node.Offset = offset
		offset += int64(BetContexts(int(node.Street))) * int64(l.Buckets(int(node.Street))) * int64(len(node.Acts))
	}
	l.BetSlots = offset
	l.DrawSlots = int64(Streets-1) * 2 * aggrStates * drawCtx * l.drawClasses * MaxCand
	return l
}

func newCompactLayout(t *Tree, a *Abstraction) *Layout {
	l := newHistoryLayout(t, a)
	type key struct {
		street, actor uint8
		wagers        int32
		commit        [2]int32
		facing        bool
		predrawNode   int
	}
	offsets := map[key]int64{}
	var slots int64
	for i := range t.Nodes {
		n := &t.Nodes[i]
		if n.Kind != KindBet {
			continue
		}
		k := key{street: n.Street, actor: n.Actor, wagers: n.Wagers, commit: n.Commit, facing: n.Facing}
		if n.Street == Predraw {
			k.predrawNode = i + 1
		}
		offset, ok := offsets[k]
		if !ok {
			offset = slots
			offsets[k] = offset
			slots += int64(BetContexts(int(n.Street)) * l.Buckets(int(n.Street)) * len(n.Acts))
		}
		n.Offset = offset
	}
	l.BetSlots = slots
	return l
}

// BetContexts is the number of draw-count contexts a street's betting
// sets distinguish.
func BetContexts(street int) int {
	switch street {
	case Predraw:
		return 1
	case Draw1:
		return betCtxDraw1
	default:
		return betCtxLate
	}
}

// Buckets is the number of hand buckets a street's betting sets use.
func Buckets(street int) int {
	switch street {
	case Predraw:
		return handclass.Num
	case Draw3:
		return NumFinalBuckets
	default:
		return NumDrawBuckets
	}
}

// Buckets reports the count for this layout, including any river refinement.
func (l *Layout) Buckets(street int) int {
	if (street == Draw1 || street == Draw2) && l.drawBuckets > 0 {
		return l.drawBuckets
	}
	if street == Draw3 && l.finalBuckets > 0 {
		return l.finalBuckets
	}
	return Buckets(street)
}

// Bucket is the hand's bucket on a street, from its class.
func (a *Abstraction) Bucket(street, class int) int {
	switch street {
	case Predraw:
		return class
	case Draw3:
		return int(a.Classes[class].Final)
	default:
		return int(a.Classes[class].Draw)
	}
}

func clip(count int) int {
	if count < 0 {
		return 0
	}
	if count > drawClip {
		return drawClip
	}
	return count
}

// BetContext folds the draw counts into a context for player p acting on
// a street. Counts are per seat per street, -1 where not yet drawn.
func BetContext(p, street int, drawn *[2]DrawCounts) int {
	switch street {
	case Predraw:
		return 0
	case Draw1:
		return clip(int(drawn[p][street]))*drawReads + clip(int(drawn[1-p][street]))
	default:
		return (clip(int(drawn[p][street]))*drawReads+clip(int(drawn[1-p][street])))*drawReads +
			clip(int(drawn[1-p][street-1]))
	}
}

// DrawContext is the drawer's public read: the opponent's count this
// street if it has drawn already (the button's view), and its count last
// street. Both are zero where nothing has been drawn yet, which the street
// and seat disambiguate.
func DrawContext(p, street int, drawn *[2]DrawCounts) int {
	now, prev := 0, 0
	if drawn[1-p][street] >= 0 {
		now = clip(int(drawn[1-p][street]))
	}
	if street > Draw1 {
		prev = clip(int(drawn[1-p][street-1]))
	}
	return now*drawReads + prev
}

// AggrState reads who made the hand's last wager from the drawer's side.
func AggrState(p int, lastAggr int) int {
	switch lastAggr {
	case p:
		return 1
	case 1 - p:
		return 2
	default:
		return 0
	}
}

// BetSlot is the first slot of a betting set.
func (l *Layout) BetSlot(node *Node, ctx, bucket int) int64 {
	return node.Offset + int64(ctx*l.Buckets(int(node.Street))+bucket)*int64(len(node.Acts))
}

// DrawSlot is the first slot of a draw set; the set holds MaxCand slots.
func (l *Layout) DrawSlot(street, p, aggr, ctx, drawClass int) int64 {
	group := (((int64(street-Draw1)*2+int64(p))*aggrStates+int64(aggr))*drawCtx + int64(ctx))
	return (group*l.drawClasses + int64(drawClass)) * MaxCand
}
