package beryl

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/lapis"
	"github.com/nuttakit/2-7-bot/internal/onyx"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

type drawCapture struct {
	view  cfr.View
	calls int
	keep  uint8
}

func (*drawCapture) Bet(*cfr.View) (int, bool) { return cfr.Pass, true }
func (m *drawCapture) Draw(v *cfr.View) (uint8, bool) {
	m.view, m.calls = *v, m.calls+1
	return m.keep, true
}

func testBot(t *testing.T, model cfr.Model) *Bot {
	t.Helper()
	base, err := onyx.New()
	if err != nil {
		t.Fatal(err)
	}
	b := newWith(base, lapis.NewModel(model))
	if b.spinel.Fallback == nil {
		t.Fatal("Spinel must fall back to the same Onyx instance")
	}
	return b
}

func TestProjectedDrawUsesStrongestLiveRepresentativeAndUnseenCurrentCount(t *testing.T) {
	model := &drawCapture{keep: 31}
	b := testBot(t, model)
	b.Hello(wire.Message{SeatCount: 6})
	b.HandStart(wire.Message{Seat: 0})
	b.Observe(wire.Event{Kind: wire.EventHandStart, Button: 0})
	b.Observe(wire.Event{Kind: wire.EventDealHole, Seat: 0, Cards: cards.MustParse("2c", "3d", "5h", "7s", "Kc")})
	b.Observe(wire.Event{Kind: wire.EventActed, Seat: 3, Action: wire.Raise(200)})
	b.Observe(wire.Event{Kind: wire.EventActed, Seat: 3, Action: wire.Fold()})
	b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: cfr.Draw1})
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 1, Count: 0})
	b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: cfr.Draw2})
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: 2, Count: 2})
	action := b.Decide(wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5})
	if action.Kind != wire.ActionDiscard || model.calls != 1 || b.Fallbacks != 0 {
		t.Fatalf("action=%+v calls=%d fallbacks=%d", action, model.calls, b.Fallbacks)
	}
	if model.view.Seat != cfr.Btn || model.view.Drawn[cfr.BB][cfr.Draw1] != 0 || model.view.Drawn[cfr.BB][cfr.Draw2] != -1 {
		t.Fatalf("projected view = %+v", model.view)
	}
	history := cfr.PolicyHistoryAt(model.view.Node)
	if history[cfr.Predraw][cfr.BB][cfr.HistoryAggressive] == 0 {
		t.Fatal("folded raiser's public pressure disappeared from projection")
	}
}

func TestHeadsUpBypassesBerylAndUsesOriginalSpinelTracker(t *testing.T) {
	model := &drawCapture{keep: 0b01111}
	b := testBot(t, model)
	b.Hello(wire.Message{SeatCount: 2})
	b.HandStart(wire.Message{Seat: cfr.Btn})
	b.Observe(wire.Event{Kind: wire.EventHandStart, Button: cfr.Btn})
	b.Observe(wire.Event{Kind: wire.EventDealHole, Seat: cfr.Btn, Cards: cards.MustParse("2c", "3d", "4h", "7s", "Kc")})
	b.Observe(wire.Event{Kind: wire.EventActed, Seat: cfr.Btn, Action: wire.Raise(200)})
	b.Observe(wire.Event{Kind: wire.EventActed, Seat: cfr.BB, Action: wire.Call()})
	b.Observe(wire.Event{Kind: wire.EventStreetStart, Street: cfr.Draw1})
	b.Observe(wire.Event{Kind: wire.EventDrawResult, Seat: cfr.BB, Count: 2})
	action := b.Decide(wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5})
	if model.calls != 1 || action.Kind != wire.ActionDiscard || len(action.Cards) != 1 || b.Fallbacks != 0 {
		t.Fatalf("action=%+v calls=%d fallbacks=%d", action, model.calls, b.Fallbacks)
	}
}

func TestPredrawExtensionsThroughEventHistory(t *testing.T) {
	tests := []struct {
		name   string
		hero   int
		hand   []cards.Card
		events []wire.Event
		want   string
	}{
		{"unopened", 3, cards.MustParse("2c", "3d", "4h", "5s", "Kc"), nil, wire.ActionRaise},
		{"limped", 5, cards.MustParse("2c", "7d", "Jh", "Qs", "Kc"), []wire.Event{{Kind: wire.EventActed, Seat: 3, Action: wire.Call()}}, wire.ActionCall},
		{"single early open", 0, cards.MustParse("2c", "4d", "8h", "Qs", "Qc"), []wire.Event{{Kind: wire.EventActed, Seat: 3, Action: wire.Raise(200)}}, wire.ActionFold},
		{"single cutoff open", 0, cards.MustParse("2c", "4d", "8h", "Qs", "Qc"), []wire.Event{{Kind: wire.EventActed, Seat: 5, Action: wire.Raise(200)}}, wire.ActionCall},
		{"reraised own chart open", 3, cards.MustParse("2c", "3d", "4h", "5s", "Kc"), []wire.Event{{Kind: wire.EventActed, Seat: 3, Action: wire.Raise(200)}, {Kind: wire.EventActed, Seat: 4, Action: wire.Raise(300)}}, wire.ActionCall},
		{"cold capped weak", 0, cards.MustParse("7c", "9d", "Jh", "Qs", "Kc"), []wire.Event{{Kind: wire.EventActed, Seat: 3, Action: wire.Raise(200)}, {Kind: wire.EventActed, Seat: 4, Action: wire.Raise(300)}, {Kind: wire.EventActed, Seat: 5, Action: wire.Raise(400)}}, wire.ActionFold},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newState()
			s.hello(wire.Message{SeatCount: 6, Stakes: wire.Stakes{BigBlind: 100}})
			s.handStart(wire.Message{Seat: tt.hero})
			s.observe(wire.Event{Kind: wire.EventHandStart, Button: 0})
			s.cards = tt.hand
			for _, event := range tt.events {
				s.observe(event)
			}
			call := uint64(100)
			if got := (&Bot{State: s}).predraw(wire.Decision{Kind: wire.DecisionWager, Call: &call}).Kind; got != tt.want {
				t.Fatalf("action = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestPredrawOverridePersistsInitialAdmissionAcrossReraise(t *testing.T) {
	model := &drawCapture{keep: 31}
	b := testBot(t, model)
	views := make([]PredrawView, 0, 2)
	b.PredrawOverride = func(view PredrawView) (PredrawChoice, bool) {
		views = append(views, view)
		return PredrawChoice{Action: wire.Call(), Admitted: true}, true
	}
	b.Hello(wire.Message{SeatCount: 6, Stakes: wire.Stakes{BigBlind: 100}})
	b.HandStart(wire.Message{Seat: 3})
	b.Observe(wire.Event{Kind: wire.EventHandStart, Button: 0})
	b.Observe(wire.Event{Kind: wire.EventPost, Seat: 1, PostKind: "small-blind", Amount: 50})
	b.Observe(wire.Event{Kind: wire.EventPost, Seat: 2, PostKind: "big-blind", Amount: 100})
	b.Observe(wire.Event{Kind: wire.EventDealHole, Seat: 3, Cards: cards.MustParse("2c", "3d", "5h", "7s", "Kc")})
	call := uint64(100)
	b.Decide(wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call, Raise: &wire.Range{MinTo: 200, MaxTo: 200}})
	b.Observe(wire.Event{Kind: wire.EventActed, Seat: 3, Action: wire.Call(), StreetCommit: 100})
	b.Observe(wire.Event{Kind: wire.EventActed, Seat: 4, Action: wire.Raise(200), StreetCommit: 200})
	b.Decide(wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call, Raise: &wire.Range{MinTo: 300, MaxTo: 300}})
	if len(views) != 2 || views[0].EntryDecided || !views[1].EntryDecided || !views[1].Admitted {
		t.Fatalf("override views = %+v", views)
	}
	if views[1].Position != UnderTheGun || views[1].Active != 0x3f || views[1].Commitments[4] != 200 || len(views[1].Actions) != 2 {
		t.Fatalf("public view incomplete: %+v", views[1])
	}
}
