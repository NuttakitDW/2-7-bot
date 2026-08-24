package wire

import (
	"encoding/json"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

// Event tags, kebab-case as on the wire (WIRE_PROTOCOL.md:302-314).
const (
	EventHandStart     = "hand-start"
	EventPost          = "post"
	EventDealHole      = "deal-hole"
	EventStreetStart   = "street-start"
	EventDealCommunity = "deal-community"
	EventDealUp        = "deal-up"
	EventActed         = "acted"
	EventDrawResult    = "draw-result"
	EventShowdownShow  = "showdown-show"
	EventPotAwarded    = "pot-awarded"
	EventHandEnd       = "hand-end"
)

// Event is one observable occurrence. Events are the single source of truth
// for everything in a hand: `act` deliberately carries no table state, so
// hole cards arrive here in deal-hole and draw-result, and every wager in
// post and acted.
//
// The union is flattened into one struct because the field names do not
// collide — `kind` inside a post event is the post kind, and the event tag
// itself lives under `event`.
type Event struct {
	Kind string `json:"event"`

	Seat   int    `json:"seat"`
	HandNo uint64 `json:"hand_no"`

	// hand-start
	Button int      `json:"button"`
	Stacks []uint64 `json:"stacks"`

	// post
	PostKind string `json:"kind"`
	Amount   uint64 `json:"amount"`
	AllIn    bool   `json:"all_in"`

	// deal-hole, deal-community, deal-up, showdown-show
	Cards []cards.Card `json:"cards"`
	Count int          `json:"count"`

	// street-start, deal-community
	Street int    `json:"street"`
	Label  string `json:"label"`

	// acted
	Action       Action `json:"action"`
	StreetCommit uint64 `json:"street_commit"`

	// draw-result. Discarded and Drawn are private to the drawing seat;
	// observers see empty lists with Count still populated, which is what
	// makes draw counts the free public read in triple draw.
	Discarded []cards.Card `json:"discarded"`
	Drawn     []cards.Card `json:"drawn"`

	// showdown-show. Hi is the engine's own HandValue for the shown hand —
	// for 27td-fl a DeuceToSevenLow value, directly comparable against
	// deuce.Eval. Lo is always null in this game.
	Hi *uint64 `json:"hi"`
	Lo *uint64 `json:"lo"`

	// pot-awarded
	Pot     int       `json:"pot"`
	Side    string    `json:"side"`
	Winners [][]int64 `json:"winners"`

	// hand-end
	Nets []int64 `json:"nets"`
}

// knownEvents is the set this build understands. Anything outside it decodes
// to a bare tag and is skipped, per the forward-compatibility rule.
var knownEvents = map[string]bool{
	EventHandStart: true, EventPost: true, EventDealHole: true,
	EventStreetStart: true, EventDealCommunity: true, EventDealUp: true,
	EventActed: true, EventDrawResult: true, EventShowdownShow: true,
	EventPotAwarded: true, EventHandEnd: true,
}

// Known reports whether this build recognizes the event.
func (e Event) Known() bool { return knownEvents[e.Kind] }

// UnmarshalJSON reads the tag first and only decodes the payload for events
// this build knows. An unrecognized tag yields an Event carrying nothing but
// its name and a nil error — a future arena may give a new event fields
// whose shapes would not survive the flat decode above, and the spec is
// explicit that must not break us.
func (e *Event) UnmarshalJSON(data []byte) error {
	var tag struct {
		Kind string `json:"event"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	if !knownEvents[tag.Kind] {
		*e = Event{Kind: tag.Kind}
		return nil
	}
	type plain Event // shed this method, keep the field tags
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*e = Event(decoded)
	return nil
}
