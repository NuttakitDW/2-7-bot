package wire

import (
	"encoding/json"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

// Lines copied verbatim from docs/protocol/WIRE_PROTOCOL.md at the pinned
// ENGINE_SHA, so the decoder is pinned to the specification's own examples
// rather than to an assumption about them.
func TestDecodeSpecExamples(t *testing.T) {
	t.Run("hello", func(t *testing.T) {
		line := `{"t":"hello","proto":1,"game_id":"holdem-nl","stakes":{"kind":"blinds","small_blind":50,"big_blind":100,"ante":0},"betting":{"kind":"no-limit"},"seat_count":2,"starting_stack":10000,"timeout_ms":5000}`
		msg := decode(t, line)
		if msg.Type != MsgHello || msg.Proto != 1 || msg.SeatCount != 2 {
			t.Fatalf("got %+v", msg)
		}
		if msg.Stakes.Kind != "blinds" || msg.Stakes.BigBlind != 100 {
			t.Errorf("stakes = %+v", msg.Stakes)
		}
		if msg.TimeoutMs == nil || *msg.TimeoutMs != 5000 {
			t.Errorf("timeout_ms = %v", msg.TimeoutMs)
		}
	})

	t.Run("fixed-limit betting carries the raise cap", func(t *testing.T) {
		line := `{"t":"hello","proto":1,"game_id":"stud-fl","stakes":{"kind":"stud","ante":20,"bring_in":50,"small_bet":100,"big_bet":200},"betting":{"kind":"fixed-limit","raise_cap":4},"seat_count":2,"starting_stack":10000,"timeout_ms":5000}`
		msg := decode(t, line)
		if msg.Betting.Kind != "fixed-limit" || msg.Betting.RaiseCap == nil || *msg.Betting.RaiseCap != 4 {
			t.Errorf("betting = %+v", msg.Betting)
		}
	})

	t.Run("act with a wager decision", func(t *testing.T) {
		line := `{"t":"act","hand_no":1,"seat":0,"decision":{"kind":"wager","fold":true,"check":false,"call":100,"raise":{"min_to":300,"max_to":10000}},"deadline_ms":5000}`
		msg := decode(t, line)
		decision := msg.Decision
		if decision.Kind != DecisionWager || !decision.Fold || decision.Check {
			t.Fatalf("decision = %+v", decision)
		}
		if decision.Call == nil || *decision.Call != 100 {
			t.Errorf("call = %v, want 100", decision.Call)
		}
		if decision.Raise == nil || decision.Raise.MinTo != 300 || decision.Raise.MaxTo != 10000 {
			t.Errorf("raise = %+v", decision.Raise)
		}
		// bet is omitted entirely, not sent as null.
		if decision.Bet != nil {
			t.Errorf("bet = %+v, want absent", decision.Bet)
		}
	})

	t.Run("act with a draw decision", func(t *testing.T) {
		line := `{"t":"act","hand_no":4,"seat":1,"decision":{"kind":"draw","max_discards":3},"deadline_ms":5000}`
		msg := decode(t, line)
		if msg.Decision.Kind != DecisionDraw || msg.Decision.MaxDiscards != 3 {
			t.Errorf("decision = %+v", msg.Decision)
		}
	})

	t.Run("deal-hole is redacted for other seats", func(t *testing.T) {
		msg := decode(t, `{"t":"event","hand_no":0,"ev":{"event":"deal-hole","seat":1,"cards":[],"count":2}}`)
		if msg.Event.Kind != EventDealHole || len(msg.Event.Cards) != 0 || msg.Event.Count != 2 {
			t.Errorf("event = %+v", msg.Event)
		}
	})

	t.Run("post carries its own kind", func(t *testing.T) {
		msg := decode(t, `{"t":"event","hand_no":1,"ev":{"event":"post","seat":0,"kind":"small-blind","amount":50,"all_in":false}}`)
		if msg.Event.Kind != EventPost || msg.Event.PostKind != "small-blind" || msg.Event.Amount != 50 {
			t.Errorf("event = %+v", msg.Event)
		}
	})

	t.Run("showdown-show carries the engine hand value", func(t *testing.T) {
		msg := decode(t, `{"t":"event","hand_no":0,"ev":{"event":"showdown-show","seat":1,"cards":["Jd","4s"],"hi":1680704,"lo":null}}`)
		if msg.Event.Hi == nil || *msg.Event.Hi != 1680704 {
			t.Fatalf("hi = %v", msg.Event.Hi)
		}
		if msg.Event.Lo != nil {
			t.Errorf("lo = %v, want null", msg.Event.Lo)
		}
		if got := cards.Strings(msg.Event.Cards); len(got) != 2 || got[0] != "Jd" {
			t.Errorf("cards = %v", got)
		}
	})

	t.Run("hand-end nets", func(t *testing.T) {
		msg := decode(t, `{"t":"hand-end","hand_no":1,"nets":[600,-600]}`)
		if len(msg.Nets) != 2 || msg.Nets[0] != 600 || msg.Nets[1] != -600 {
			t.Errorf("nets = %v", msg.Nets)
		}
	})
}

// The forward-compatibility contract: a newer arena may add message types and
// event types, and neither may be treated as an error.
func TestUnknownTagsAreNoOps(t *testing.T) {
	t.Run("unknown message type", func(t *testing.T) {
		msg, err := DecodeMessage([]byte(`{"t":"table-talk","mood":{"deep":true}}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if msg.Known() {
			t.Errorf("Known() = true for %q", msg.Type)
		}
	})

	t.Run("unknown event type with an incompatible field shape", func(t *testing.T) {
		// "cards" as an object rather than an array would break a flat
		// decode, which is exactly why the tag is read first.
		msg, err := DecodeMessage([]byte(`{"t":"event","hand_no":3,"ev":{"event":"rabbit-hunt","cards":{"burned":3}}}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if msg.Event.Known() {
			t.Errorf("Known() = true for %q", msg.Event.Kind)
		}
	})
}

func TestActionEncoding(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		want   string
	}{
		{"fold", Fold(), `{"kind":"fold"}`},
		{"check", Check(), `{"kind":"check"}`},
		{"call", Call(), `{"kind":"call"}`},
		{"raise carries a total", Raise(300), `{"kind":"raise","to":300}`},
		{"bet carries a total", Bet(100), `{"kind":"bet","to":100}`},
		{"bring-in", BringIn(), `{"kind":"bring-in"}`},
		{"discard", Discard(cards.MustParse("2c", "7h")), `{"kind":"discard","cards":["2c","7h"]}`},
		// A stand pat is an empty list, never an absent field.
		{"stand pat", Discard(nil), `{"kind":"discard","cards":[]}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.action)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(encoded) != test.want {
				t.Errorf("got %s, want %s", encoded, test.want)
			}
		})
	}
}

func TestReplyEncoding(t *testing.T) {
	encoded, err := json.Marshal(Reply(Raise(300)))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `{"t":"action","action":{"kind":"raise","to":300}}`; string(encoded) != want {
		t.Errorf("got %s, want %s", encoded, want)
	}
	if encoded, _ = json.Marshal(Join()); string(encoded) != `{"t":"join"}` {
		t.Errorf("join = %s", encoded)
	}
}

// The anti-fault test: every decision the arena can present, crossed with
// every action the policy could ask for, must produce something legal.
func TestLegalizeIsAlwaysLegal(t *testing.T) {
	call := uint64(100)
	small := &Range{MinTo: 100, MaxTo: 100}
	big := &Range{MinTo: 200, MaxTo: 200}
	complete := &Range{MinTo: 20, MaxTo: 20}
	bringIn := uint64(10)

	decisions := []struct {
		name     string
		decision Decision
	}{
		{"open, may bet or check", Decision{Kind: DecisionWager, Check: true, Bet: small}},
		{"facing a bet", Decision{Kind: DecisionWager, Fold: true, Call: &call, Raise: big}},
		{"facing a capped bet", Decision{Kind: DecisionWager, Fold: true, Call: &call}},
		{"check only", Decision{Kind: DecisionWager, Check: true}},
		{"draw", Decision{Kind: DecisionDraw, MaxDiscards: 5}},
		{"draw, nothing allowed", Decision{Kind: DecisionDraw}},
		{"bring-in", Decision{Kind: DecisionBringIn, BringIn: &bringIn, Complete: complete}},
		{"unknown kind", Decision{Kind: "telepathy"}},
	}

	held := cards.MustParse("2c", "7h", "9d", "Js", "Ac")
	wants := []Action{
		Fold(), Check(), Call(), Bet(100), Raise(200), BringIn(),
		Discard(held), Discard(nil),
		// A policy bug: cards not held, a duplicate, and too many of them.
		Discard(cards.MustParse("2c", "2c", "3d", "7h", "9d", "Js", "Ac")),
		{Kind: "shove-everything"},
	}

	for _, presented := range decisions {
		for _, want := range wants {
			name := presented.name + "/" + want.Kind
			t.Run(name, func(t *testing.T) {
				got := Legalize(presented.decision, want, held)
				if !isLegal(presented.decision, got, held) {
					t.Errorf("Legalize(%+v, %+v) = %+v, which is not legal",
						presented.decision, want, got)
				}
			})
		}
	}
}

func TestLegalizeDrawSanitises(t *testing.T) {
	held := cards.MustParse("2c", "7h", "9d", "Js", "Ac")

	tests := []struct {
		name        string
		maxDiscards int
		want        []string
		expect      []string
	}{
		{"passes a valid list through", 5, []string{"Js", "Ac"}, []string{"Js", "Ac"}},
		{"drops cards not held", 5, []string{"Js", "Kd"}, []string{"Js"}},
		{"drops a repeated card", 5, []string{"Js", "Js"}, []string{"Js"}},
		{"truncates to max_discards", 2, []string{"Js", "Ac", "9d"}, []string{"Js", "Ac"}},
		{"stand pat stays empty", 5, nil, []string{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := Decision{Kind: DecisionDraw, MaxDiscards: test.maxDiscards}
			got := Legalize(decision, Discard(cards.MustParse(test.want...)), held)
			if got.Kind != ActionDiscard {
				t.Fatalf("kind = %q", got.Kind)
			}
			if names := cards.Strings(got.Cards); !equalStrings(names, test.expect) {
				t.Errorf("discards = %v, want %v", names, test.expect)
			}
		})
	}
}

// isLegal re-derives legality from the decision independently of Legalize, so
// the two would have to be wrong in the same way to agree wrongly.
func isLegal(decision Decision, action Action, held []cards.Card) bool {
	switch decision.Kind {
	case DecisionWager:
		switch action.Kind {
		case ActionFold:
			return decision.Fold
		case ActionCheck:
			return decision.Check
		case ActionCall:
			return decision.Call != nil
		case ActionBet:
			return decision.Bet != nil && action.To >= decision.Bet.MinTo && action.To <= decision.Bet.MaxTo
		case ActionRaise:
			return decision.Raise != nil && action.To >= decision.Raise.MinTo && action.To <= decision.Raise.MaxTo
		}
		return false
	case DecisionDraw:
		if action.Kind != ActionDiscard || len(action.Cards) > decision.MaxDiscards {
			return false
		}
		remaining := make([]cards.Card, len(held))
		copy(remaining, held)
		for _, card := range action.Cards {
			before := len(remaining)
			remaining = cards.Without(remaining, []cards.Card{card})
			if len(remaining) != before-1 {
				return false // not held, or listed twice
			}
		}
		return true
	case DecisionBringIn:
		switch action.Kind {
		case ActionBringIn:
			return true
		case ActionBet:
			return decision.Complete != nil &&
				action.To >= decision.Complete.MinTo && action.To <= decision.Complete.MaxTo
		}
		return false
	default:
		// Nothing is verifiably legal against a decision we cannot read;
		// the guard's job there is only to answer at all.
		return action.Kind == ActionCheck
	}
}

func decode(t *testing.T, line string) Message {
	t.Helper()
	msg, err := DecodeMessage([]byte(line))
	if err != nil {
		t.Fatalf("DecodeMessage(%s): %v", line, err)
	}
	return msg
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
