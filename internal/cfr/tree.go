// Package cfr trains a heads-up 27td-fl blueprint by external-sampling
// Monte Carlo CFR over an abstracted game, and plays the result.
//
// The public part of the game — every betting sequence a hand can take,
// with its draw phases in between — is small enough to enumerate outright:
// a few thousand nodes. Only the cards are abstracted (abstract.go). The
// tree here is exact and mirrors the engine's rules in docs/game/rules.md:
// blinds 50/100, small bets on predraw and draw1, big bets after, a cap of
// four wagers a street counting the big blind, the big blind drawing and
// acting first on every draw street, the button acting first predraw.
package cfr

// Seats. The button is seat 0 on the arena, so the same numbering is used
// throughout: it posts the small blind, acts first predraw, and draws and
// acts last on every draw street.
const (
	Btn = 0
	BB  = 1
)

// Streets.
const (
	Predraw = 0
	Draw1   = 1
	Draw2   = 2
	Draw3   = 3
	Streets = 4
)

// Stakes, in chips. 27td-fl on the arena runs at these and nothing here
// reads them from the wire, so a match at other stakes plays the same
// shape scaled — fixed limit has no sizing decision to get wrong.
const (
	SmallBlind = 50
	BigBlind   = 100
	SmallBet   = 100
	BigBet     = 200
	RaiseCap   = 4
)

// Betting actions, in the order a node's Acts lists them.
const (
	Fold = 0
	Pass = 1 // check, or call when facing a wager
	Aggr = 2 // bet, or raise when facing a wager
)

// Node kinds.
const (
	KindBet = iota
	KindDraw
	KindFold
	KindShowdown
)

// Node is one public state of the hand.
type Node struct {
	Kind   uint8
	Street uint8
	// Actor is the player to act (KindBet, KindDraw) or the folder (KindFold).
	Actor uint8
	// Facing reports whether a wager is outstanding, which is what makes
	// Fold legal and turns Pass into a call.
	Facing bool
	// Acts are the legal betting actions at a KindBet node, in fixed order.
	Acts []uint8
	// Next is the child per action index for KindBet; Next[0] for KindDraw.
	Next [3]int32
	// Commit is each player's total contribution to the pot on entry.
	Commit [2]int32
	// Wagers is the count of wagers made on this street so far, the big
	// blind counting as the first predraw.
	Wagers int32
	// Offset is the node's first slot in the betting strategy tables, or
	// -1 for a node that stores no strategy.
	Offset int64
}

// Tree is the enumerated public game.
type Tree struct {
	Nodes []Node
	// Root is the predraw betting root: the button facing the big blind.
	// Every other node is reached by walking; each betting prefix owns its
	// own draw nodes, so there is no per-street entry point.
	Root      int32
	betNodes  int
	drawNodes int
}

// BuildTree enumerates the game.
func BuildTree() *Tree {
	t := &Tree{}
	t.Root = t.build(Predraw, [2]int32{SmallBlind, BigBlind}, BigBlind, 1, Btn, [2]bool{false, false})
	return t
}

// build creates the betting node for a round state and, recursively, all of
// its descendants. Round state: committed chips this street per player,
// the wager level to match, wagers made, who acts, and who has acted
// voluntarily this street. Commit accumulates across streets.
func (t *Tree) build(street int, commit [2]int32, level int32, wagers int32, actor int, acted [2]bool) int32 {
	id := int32(len(t.Nodes))
	facing := commit[actor] < level
	node := Node{Kind: KindBet, Street: uint8(street), Actor: uint8(actor), Facing: facing,
		Commit: commit, Wagers: wagers, Offset: -1}
	if facing {
		node.Acts = append(node.Acts, Fold, Pass)
	} else {
		node.Acts = append(node.Acts, Pass)
	}
	if wagers < RaiseCap {
		node.Acts = append(node.Acts, Aggr)
	}
	t.Nodes = append(t.Nodes, node)
	t.betNodes++

	other := 1 - actor
	for i, act := range t.Nodes[id].Acts {
		var child int32
		switch act {
		case Fold:
			child = t.terminal(KindFold, street, actor, commit)
		case Pass:
			next := commit
			next[actor] = level
			nextActed := acted
			nextActed[actor] = true
			if acted[other] {
				child = t.roundEnd(street, next)
			} else {
				child = t.build(street, next, level, wagers, other, nextActed)
			}
		case Aggr:
			next := commit
			nextLevel := level + betSize(street)
			next[actor] = nextLevel
			nextActed := acted
			nextActed[actor] = true
			child = t.build(street, next, nextLevel, wagers+1, other, nextActed)
		}
		t.Nodes[id].Next[i] = child
	}
	return id
}

// roundEnd is what follows a completed betting round: showdown after the
// last street, otherwise the next street's draw phase, big blind first.
func (t *Tree) roundEnd(street int, commit [2]int32) int32 {
	if street == Draw3 {
		return t.terminal(KindShowdown, street, 0, commit)
	}
	next := street + 1
	bb := int32(len(t.Nodes))
	t.Nodes = append(t.Nodes, Node{Kind: KindDraw, Street: uint8(next), Actor: BB, Commit: commit, Offset: -1})
	btn := int32(len(t.Nodes))
	t.Nodes = append(t.Nodes, Node{Kind: KindDraw, Street: uint8(next), Actor: Btn, Commit: commit, Offset: -1})
	t.drawNodes += 2
	t.Nodes[bb].Next[0] = btn
	// Draw streets bet from a clean round: nothing committed to match, the
	// big blind first, nobody acted yet.
	bet := t.build(next, commit, commit[Btn], 0, BB, [2]bool{false, false})
	t.Nodes[btn].Next[0] = bet
	return bb
}

func (t *Tree) terminal(kind uint8, street, actor int, commit [2]int32) int32 {
	id := int32(len(t.Nodes))
	t.Nodes = append(t.Nodes, Node{Kind: kind, Street: uint8(street), Actor: uint8(actor), Commit: commit, Offset: -1})
	return id
}

// betSize is the wager increment on a street.
func betSize(street int) int32 {
	if street >= Draw2 {
		return BigBet
	}
	return SmallBet
}

// Payoff is the chips player p wins or loses at a terminal node, given who
// won a showdown (0 or 1, or -1 for a split).
func (n *Node) Payoff(p int, showdownWinner int) int32 {
	switch n.Kind {
	case KindFold:
		if int(n.Actor) == p {
			return -n.Commit[p]
		}
		return n.Commit[1-p]
	case KindShowdown:
		switch showdownWinner {
		case p:
			return n.Commit[1-p]
		case 1 - p:
			return -n.Commit[p]
		default:
			return 0
		}
	}
	panic("cfr: Payoff of a non-terminal node")
}

// Counts reports how many betting and draw nodes the tree holds.
func (t *Tree) Counts() (bet, draw int) { return t.betNodes, t.drawNodes }
