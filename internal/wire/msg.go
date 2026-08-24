package wire

import "encoding/json"

// The arena→bot message tags.
const (
	MsgHello     = "hello"
	MsgJoined    = "joined"
	MsgHandStart = "hand-start"
	MsgEvent     = "event"
	MsgAct       = "act"
	MsgHandEnd   = "hand-end"
	MsgMatchEnd  = "match-end"
)

// MaxLineBytes is the protocol's line cap. A longer line is a violation and
// a well-behaved peer disconnects rather than recover mid-stream
// (WIRE_PROTOCOL.md:41-43); we size our reader to it so a legitimate long
// line is never truncated into malformed JSON.
const MaxLineBytes = 65536

// Stakes is the per-match stakes, tagged on kind. 27td-fl is a blind game,
// so `blinds` is the only shape it ever sends.
type Stakes struct {
	Kind       string `json:"kind"`
	SmallBlind uint64 `json:"small_blind"`
	BigBlind   uint64 `json:"big_blind"`
	Ante       uint64 `json:"ante"`

	// Stud only, and never populated for this game.
	BringIn  uint64 `json:"bring_in"`
	SmallBet uint64 `json:"small_bet"`
	BigBet   uint64 `json:"big_bet"`
}

// Betting is the betting structure. 27td-fl is fixed-limit with
// RaiseCap 4 (game/spec.rs:672), the cap counting wagers with the opening
// bet or blind as the first — so a capped street is a bet plus three raises.
type Betting struct {
	Kind     string `json:"kind"`
	RaiseCap *uint8 `json:"raise_cap"`
}

// Message is one decoded arena→bot line. Like Event it is a flattened union;
// the per-message fields do not collide.
type Message struct {
	Type string `json:"t"`

	// hello
	Proto         uint32  `json:"proto"`
	GameID        string  `json:"game_id"`
	Stakes        Stakes  `json:"stakes"`
	Betting       Betting `json:"betting"`
	SeatCount     int     `json:"seat_count"`
	StartingStack uint64  `json:"starting_stack"`
	TimeoutMs     *uint64 `json:"timeout_ms"`

	// joined
	Name string `json:"name"`

	// hand-start, event, act, hand-end
	HandNo uint64 `json:"hand_no"`
	Seat   int    `json:"seat"`

	// event
	Event Event `json:"ev"`

	// act
	Decision   Decision `json:"decision"`
	DeadlineMs *uint64  `json:"deadline_ms"`

	// hand-end
	Nets []int64 `json:"nets"`
}

var knownMessages = map[string]bool{
	MsgHello: true, MsgJoined: true, MsgHandStart: true,
	MsgEvent: true, MsgAct: true, MsgHandEnd: true, MsgMatchEnd: true,
}

// Known reports whether this build recognizes the message.
func (m Message) Known() bool { return knownMessages[m.Type] }

// DecodeMessage reads one line. An unrecognized "t" yields a Message
// carrying nothing but its tag and a nil error, so the caller's switch can
// simply not match it and keep reading.
func DecodeMessage(line []byte) (Message, error) {
	var tag struct {
		Type string `json:"t"`
	}
	if err := json.Unmarshal(line, &tag); err != nil {
		return Message{}, err
	}
	if !knownMessages[tag.Type] {
		return Message{Type: tag.Type}, nil
	}
	var decoded Message
	if err := json.Unmarshal(line, &decoded); err != nil {
		return Message{}, err
	}
	return decoded, nil
}
