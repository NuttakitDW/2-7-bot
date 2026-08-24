package wire

// Decision kinds, kebab-case as on the wire.
const (
	DecisionWager   = "wager"
	DecisionDraw    = "draw"
	DecisionBringIn = "bring-in"
)

// Range is a legal total-commitment window. Fixed limit always sends
// MinTo == MaxTo, meaning there is exactly one legal total — send that
// number, there is no range to pick from (WIRE_PROTOCOL.md:344-347).
type Range struct {
	MinTo uint64 `json:"min_to"`
	MaxTo uint64 `json:"max_to"`
}

// Decision is what the arena says is legal right now, tagged on kind. It is
// the only state `act` carries, and it is deliberately authoritative: a bot
// must never derive legality itself.
//
// Call, Bet and Raise are omitted from the JSON entirely — not sent as null —
// when they do not apply, so they are pointers and presence is the test.
type Decision struct {
	Kind string `json:"kind"`

	// wager. Fold is offered only when there is something to call:
	// open-folding for free is never legal, so a policy that wants to fold
	// with Fold false must check instead.
	Fold  bool    `json:"fold"`
	Check bool    `json:"check"`
	Call  *uint64 `json:"call"`
	Bet   *Range  `json:"bet"`
	Raise *Range  `json:"raise"`

	// draw. For 27td-fl this is 5, not the 3 in the protocol doc's
	// transcript — that example belongs to another game
	// (docs/game/rules.md). You may discard your entire hand.
	MaxDiscards int `json:"max_discards"`

	// bring-in. Stud only; 27td-fl never sends this, but a decoder that
	// chokes on it would be wrong for the wrong reason.
	BringIn  *uint64 `json:"bring_in"`
	Complete *Range  `json:"complete"`
}
