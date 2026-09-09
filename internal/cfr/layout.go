package cfr

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/handclass"
)

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
	// FixedGroups is how many big-blind-card groups the button's sets are
	// split into (fixedProfile "button" or "early"), 1 when they are
	// not. Group 0 is the button not knowing the card; baseBet and
	// baseDraw are the size of group 0's tables, which "button" repeats
	// whole per group while "early" appends only the button's predraw
	// and first-draw sets, earlyBet and earlyDraw slots per group.
	FixedGroups         int
	baseBet, baseDraw   int64
	earlyBet, earlyDraw int64
	early               bool
	// drawClasses is the draw-class count the draw index arithmetic uses.
	drawClasses  int64
	finalBuckets int
	drawBuckets  int
	draw2Buckets int
}

// WithoutFixed returns the same abstraction and node offsets with a single
// strategy group. It can read the baseline opponent or prior while a learner
// trains separate groups for the observed big-blind card.
func (l *Layout) WithoutFixed() *Layout {
	base := *l
	if l.FixedGroups > 1 {
		base.BetSlots, base.DrawSlots = l.baseBet, l.baseDraw
	}
	base.FixedGroups = 1
	base.early = false
	base.earlyBet, base.earlyDraw = 0, 0
	return &base
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

// fixedProfile "button" gives the button separate strategy slices per group
// of the big blind's constant first card (State.DealFixed) on every
// street; "early" only on the predraw and first-draw streets, where the
// card moves the ranges most, at a fraction of the table; "none" keeps a
// single strategy.
var fixedProfile = "none"

// fixedLastStreet is the last street the "early" profile slices.
const fixedLastStreet = Draw1

// NumFixedGroups counts the big-blind-card groups: unknown, then the
// deuce through the seven singly, eights with nines, and tens and above.
const NumFixedGroups = 9

// FixedGroup maps the big blind's constant card to its strategy group.
func FixedGroup(rank cards.Rank) int {
	switch {
	case rank <= cards.Seven:
		return int(rank-cards.Two) + 1
	case rank <= cards.Nine:
		return 7
	default:
		return 8
	}
}

// NewLayout assigns every betting node its offset and sizes the tables.
func NewLayout(t *Tree, a *Abstraction) *Layout {
	var l *Layout
	switch layoutProfile {
	case "history", "history-rich":
		l = newHistoryLayout(t, a)
	case "compact", "compact-rich":
		l = newCompactLayout(t, a)
	default:
		panic("unknown CFR layout profile: " + layoutProfile)
	}
	switch fixedProfile {
	case "none":
	case "button":
		l.FixedGroups = NumFixedGroups
		l.BetSlots *= NumFixedGroups
		l.DrawSlots *= NumFixedGroups
	case "early":
		l.FixedGroups, l.early = NumFixedGroups, true
		var offset int64
		for i := range t.Nodes {
			node := &t.Nodes[i]
			if l.sliced(node) {
				node.FixedOffset = offset
				offset += int64(BetContexts(int(node.Street))) * int64(l.Buckets(int(node.Street))) * int64(len(node.Acts))
			}
		}
		l.earlyBet = offset
		l.earlyDraw = int64(fixedLastStreet-Draw1+1) * 2 * aggrStates * drawCtx * l.drawClasses * MaxCand
		l.BetSlots += int64(NumFixedGroups-1) * l.earlyBet
		l.DrawSlots += int64(NumFixedGroups-1) * l.earlyDraw
	default:
		panic("unknown CFR fixed-card profile: " + fixedProfile)
	}
	return l
}

func newHistoryLayout(t *Tree, a *Abstraction) *Layout {
	l := &Layout{drawClasses: int64(a.NumDrawClasses), finalBuckets: a.FinalBuckets, drawBuckets: a.DrawBuckets, draw2Buckets: a.Draw2Buckets}
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
	l.FixedGroups, l.baseBet, l.baseDraw = 1, l.BetSlots, l.DrawSlots
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
	l.baseBet = slots
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
	if street == Draw2 && l.draw2Buckets > 0 {
		return l.draw2Buckets
	}
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
	case Draw2:
		if a.Draw2Buckets > 0 {
			return int(a.Classes[class].Draw2)
		}
		return int(a.Classes[class].Draw)
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

// BetSlot is the first slot of a betting set, in the button's group 0.
func (l *Layout) BetSlot(node *Node, ctx, bucket int) int64 {
	return node.Offset + int64(ctx*l.Buckets(int(node.Street))+bucket)*int64(len(node.Acts))
}

// sliced reports whether a node has its own set per big-blind-card
// group under the "early" profile.
func (l *Layout) sliced(node *Node) bool {
	return node.Kind == KindBet && node.Actor == Btn && int(node.Street) <= fixedLastStreet
}

// Sliced reports whether a node's sets differ between group 0 and group.
func (l *Layout) Sliced(node *Node, group int) bool {
	if group <= 0 || group >= l.FixedGroups || node.Actor != Btn {
		return false
	}
	return !l.early || l.sliced(node)
}

// BetSlotFixed is BetSlot in the button's slice for a big-blind-card
// group; the big blind's own sets have one slice.
func (l *Layout) BetSlotFixed(node *Node, ctx, bucket, group int) int64 {
	if !l.Sliced(node, group) {
		return l.BetSlot(node, ctx, bucket)
	}
	set := int64(ctx*l.Buckets(int(node.Street))+bucket) * int64(len(node.Acts))
	if l.early {
		return l.baseBet + int64(group-1)*l.earlyBet + node.FixedOffset + set
	}
	return node.Offset + set + int64(group)*l.baseBet
}

// DrawSlot is the first slot of a draw set; the set holds MaxCand slots.
func (l *Layout) DrawSlot(street, p, aggr, ctx, drawClass int) int64 {
	group := (((int64(street-Draw1)*2+int64(p))*aggrStates+int64(aggr))*drawCtx + int64(ctx))
	return (group*l.drawClasses + int64(drawClass)) * MaxCand
}

// DrawSlotFixed is DrawSlot in the button's slice for a big-blind-card group.
func (l *Layout) DrawSlotFixed(street, p, aggr, ctx, drawClass, group int) int64 {
	slot := l.DrawSlot(street, p, aggr, ctx, drawClass)
	if group <= 0 || group >= l.FixedGroups || p != Btn {
		return slot
	}
	if l.early {
		if street > fixedLastStreet {
			return slot
		}
		return l.baseDraw + int64(group-1)*l.earlyDraw + slot
	}
	return slot + int64(group)*l.baseDraw
}
