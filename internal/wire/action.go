// Package wire is the poker-arena JSON-Lines protocol (v1), transcribed from
// docs/protocol/WIRE_PROTOCOL.md, plus the legality guard that keeps a
// strategy bug from becoming a fault.
//
// Two framing rules the decoder must honour, both from the spec's
// forward-compatibility contract: unknown JSON fields are ignored, and an
// unrecognized message "t" or event "event" tag is a no-op rather than an
// error. A newer arena has to be able to add either without breaking us.
package wire

import (
	"encoding/json"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

// The bot→arena message tags.
const (
	MsgJoin   = "join"
	MsgAction = "action"
)

// Action kinds, kebab-case as on the wire.
const (
	ActionFold    = "fold"
	ActionCheck   = "check"
	ActionCall    = "call"
	ActionBet     = "bet"
	ActionRaise   = "raise"
	ActionBringIn = "bring-in"
	ActionDiscard = "discard"
)

// Action is one decision, tagged on kind.
//
// To is a *total* street commitment after the action, never an increment
// (WIRE_PROTOCOL.md:321-327). In fixed limit every wager range has
// min_to == max_to, so there is no sizing decision anywhere in 27td-fl —
// send the one number offered.
type Action struct {
	Kind  string       `json:"kind"`
	To    uint64       `json:"to"`
	Cards []cards.Card `json:"cards"`
}

// Fold, Check, Call, BringIn and Pat are the actions carrying no payload.
func Fold() Action    { return Action{Kind: ActionFold} }
func Check() Action   { return Action{Kind: ActionCheck} }
func Call() Action    { return Action{Kind: ActionCall} }
func BringIn() Action { return Action{Kind: ActionBringIn} }

// Bet and Raise commit a total of `to` chips on this street.
func Bet(to uint64) Action   { return Action{Kind: ActionBet, To: to} }
func Raise(to uint64) Action { return Action{Kind: ActionRaise, To: to} }

// Discard throws these cards away; an empty list is standing pat.
func Discard(discards []cards.Card) Action {
	return Action{Kind: ActionDiscard, Cards: discards}
}

// MarshalJSON writes exactly the fields the chosen kind carries. A `to` on a
// call, or a missing `cards` on a stand pat, is a malformed action and
// therefore a fault, so the shape is built per kind rather than trusted to
// struct tags.
func (a Action) MarshalJSON() ([]byte, error) {
	switch a.Kind {
	case ActionBet, ActionRaise:
		return json.Marshal(struct {
			Kind string `json:"kind"`
			To   uint64 `json:"to"`
		}{a.Kind, a.To})
	case ActionDiscard:
		// Never nil: a stand pat is an empty list, not an absent field.
		discards := a.Cards
		if discards == nil {
			discards = []cards.Card{}
		}
		return json.Marshal(struct {
			Kind  string       `json:"kind"`
			Cards []cards.Card `json:"cards"`
		}{a.Kind, discards})
	default:
		return json.Marshal(struct {
			Kind string `json:"kind"`
		}{a.Kind})
	}
}

// BotMsg is a bot→arena line: either the bare join or one action.
type BotMsg struct {
	Type   string  `json:"t"`
	Action *Action `json:"action,omitempty"`
}

// Join is the ready signal sent once, immediately after hello. It carries no
// fields — identity is operator-assigned and announced back in `joined`.
func Join() BotMsg { return BotMsg{Type: MsgJoin} }

// Reply wraps an action for sending.
func Reply(action Action) BotMsg { return BotMsg{Type: MsgAction, Action: &action} }
