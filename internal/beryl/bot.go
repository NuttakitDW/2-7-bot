// Package beryl combines a displayed six-max starting chart with Spinel's
// fitted heads-up draw policy and Onyx's existing multiway betting rules.
package beryl

import (
	"fmt"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/lapis"
	"github.com/nuttakit/2-7-bot/internal/onyx"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

type Bot struct {
	State *state
	Base  *onyx.Bot
	// PredrawOverride replaces only six-seat predraw betting. It receives
	// hero-private cards and button-relative public state, never opponents'
	// private cards. The first result freezes admission for the whole round.
	PredrawOverride func(PredrawView) (PredrawChoice, bool)
	spinel          *lapis.Bot
	Fallbacks       int
}

type PredrawAction struct {
	Position Position
	Action   string
	Commit   uint64
}

type PredrawView struct {
	Hand         []cards.Card
	Position     Position
	Decision     wire.Decision
	Active       uint8
	Commitments  [seatCount]uint64
	Wagers       int
	Raises       int
	Calls        int
	Actions      []PredrawAction
	EntryDecided bool
	Admitted     bool
}

type PredrawChoice struct {
	Action   wire.Action
	Admitted bool
}

func New() (*Bot, error) {
	base, err := onyx.New()
	if err != nil {
		return nil, err
	}
	spinel, err := lapis.NewBlockerRiverResponse()
	if err != nil {
		return nil, fmt.Errorf("beryl: spinel: %w", err)
	}
	return newWith(base, spinel), nil
}

// NewDeterministic builds the six-seat Beryl continuation used by offline EV
// ranking and training while sharing the already-decoded frozen draw model.
func NewDeterministic(drawModel cfr.Model, seed uint64) (*Bot, error) {
	if drawModel == nil {
		return nil, fmt.Errorf("beryl: nil draw model")
	}
	base, err := onyx.NewSeeded(seed)
	if err != nil {
		return nil, err
	}
	return newWith(base, lapis.NewModelSeed(drawModel, seed^0xA0761D6478BD642F)), nil
}

func newWith(base *onyx.Bot, spinel *lapis.Bot) *Bot {
	spinel.Fallback = base.Decide
	return &Bot{State: newState(), Base: base, spinel: spinel}
}

func (b *Bot) Hello(m wire.Message) {
	b.State.hello(m)
	b.Base.Hello(m)
	b.spinel.Hello(m)
}

func (b *Bot) HandStart(m wire.Message) {
	b.State.handStart(m)
	b.Base.HandStart(m)
	b.spinel.HandStart(m)
}

func (b *Bot) Observe(e wire.Event) {
	b.State.observe(e)
	b.Base.Observe(e)
	b.spinel.Observe(e)
}

func (b *Bot) Decide(d wire.Decision) wire.Action {
	// Beryl's custom range is six-max only. Preserve Spinel exactly in its
	// original heads-up game, including its blocker-aware river response.
	if b.State.seats == 2 {
		before := b.spinel.Fallbacks
		action := b.spinel.Decide(d)
		b.Fallbacks += b.spinel.Fallbacks - before
		return action
	}
	if b.State.seats != seatCount || len(b.State.cards) != 5 {
		b.Fallbacks++
		return b.Base.Decide(d)
	}
	if d.Kind == wire.DecisionWager && b.State.street == cfr.Predraw {
		if b.PredrawOverride != nil {
			view := b.predrawView(d)
			if choice, ok := b.PredrawOverride(view); ok {
				if !b.State.entryDecided {
					b.State.entryDecided = true
					b.State.admitted = choice.Admitted
				}
				return wire.Legalize(d, choice.Action, b.State.cards)
			}
		}
		return wire.Legalize(d, b.predraw(d), b.State.cards)
	}
	if d.Kind == wire.DecisionDraw {
		if action, ok := b.projectedDraw(); ok {
			return wire.Legalize(d, action, b.State.cards)
		}
		b.Fallbacks++
	}
	return b.Base.Decide(d)
}

func (b *Bot) predrawView(d wire.Decision) PredrawView {
	s := b.State
	view := PredrawView{
		Hand: append([]cards.Card(nil), s.cards...), Position: s.position(s.hero), Decision: d,
		Wagers: s.predrawRaises + 1, Raises: s.predrawRaises, Calls: s.predrawCalls,
		EntryDecided: s.entryDecided, Admitted: s.admitted,
	}
	for seat := 0; seat < seatCount; seat++ {
		position := s.position(seat)
		if !s.folded[seat] {
			view.Active |= 1 << position
		}
		view.Commitments[position] = s.commitments[seat]
	}
	for _, record := range s.actions {
		if record.street == cfr.Predraw {
			view.Actions = append(view.Actions, PredrawAction{Position: s.position(record.seat), Action: record.action, Commit: record.commit})
		}
	}
	return view
}

func (b *Bot) predraw(d wire.Decision) wire.Action {
	s := b.State
	hand, heroPos := s.cards, s.position(s.hero)
	if s.predrawRaises == 0 {
		if s.predrawCalls == 0 {
			if openingPlayable(hand, heroPos) {
				return wire.Raise(0)
			}
			return wire.Fold()
		}
		// The source chart does not specify limpers. Isolate them with a
		// conservative extension: strong one-card-or-better hands raise and
		// chart entries overlimp.
		if pat96(hand) || anySubset(uniqueRanks(hand), 4, hijackFour) {
			return wire.Raise(0)
		}
		if openingPlayable(hand, heroPos) {
			return wire.Call()
		}
		return wire.Fold()
	}
	if s.predrawRaises == 1 && s.firstOpener >= 0 && s.firstOpener != s.hero {
		switch facingOpenAction(hand, heroPos, s.position(s.firstOpener)) {
		case rangeRaise:
			return wire.Raise(0)
		case rangeCall:
			return wire.Call()
		default:
			return wire.Fold()
		}
	}
	// The chart stops after one open. Against multiple raises, raise only
	// the top pat range, call made nines and clean one-card draws, and keep
	// a prior voluntary chart entry from folding to one small-bet increment
	// after its own open.
	if pat96(hand) {
		return wire.Raise(0)
	}
	cheap := d.Call != nil && *d.Call <= s.bigBlind
	if patNine(hand) || anySubset(uniqueRanks(hand), 4, eightNoStraightDraw) || cheap && s.heroVoluntary && openingPlayable(hand, heroPos) {
		return wire.Call()
	}
	return wire.Fold()
}

func (b *Bot) projectedDraw() (wire.Action, bool) {
	s := b.State
	representative, ok := s.representativeOpponent()
	if !ok || s.street < cfr.Draw1 || s.street > cfr.Draw3 {
		return wire.Action{}, false
	}
	heroSeat := cfr.BB
	if s.drawOrder(s.hero) > s.drawOrder(representative) {
		heroSeat = cfr.Btn
	}
	opponentSeat := 1 - heroSeat
	history := s.projectedHistory(heroSeat)
	last := -1
	if s.lastAggressor >= 0 {
		last = opponentSeat
		if s.lastAggressor == s.hero {
			last = heroSeat
		}
	}
	node, ok := cfr.ProjectDrawNode(s.street, heroSeat, history, last)
	if !ok {
		return wire.Action{}, false
	}
	var drawn [2]cfr.DrawCounts
	for seat := range drawn {
		for street := range drawn[seat] {
			drawn[seat][street] = -1
		}
	}
	for street := cfr.Draw1; street <= cfr.Draw3; street++ {
		drawn[heroSeat][street] = s.draws[s.hero][street]
		drawn[opponentSeat][street] = s.draws[representative][street]
	}
	return b.spinel.ProjectedDraw(node, heroSeat, s.cards, drawn, last)
}

func (s *state) projectedHistory(heroSeat int) cfr.PolicyHistory {
	var history cfr.PolicyHistory
	for _, record := range s.actions {
		if record.street < cfr.Predraw || record.street >= cfr.Streets {
			continue
		}
		seat := 1 - heroSeat
		if record.seat == s.hero {
			seat = heroSeat
		}
		action := -1
		switch record.action {
		case wire.ActionBet, wire.ActionRaise:
			action = cfr.HistoryAggressive
		case wire.ActionCheck:
			action = cfr.HistoryCheck
		case wire.ActionCall:
			action = cfr.HistoryCall
		}
		if action >= 0 && history[record.street][seat][action] < 255 {
			history[record.street][seat][action]++
		}
	}
	return history
}

func (s *state) representativeOpponent() (int, bool) {
	best, bestCount, bestAgo := -1, 6, cfr.Streets+1
	for seat := 0; seat < s.seats && seat < seatCount; seat++ {
		if seat == s.hero || s.folded[seat] {
			continue
		}
		count, ago := 6, cfr.Streets+1
		for street := min(s.street, cfr.Draw3); street >= cfr.Draw1; street-- {
			if s.draws[seat][street] >= 0 {
				count, ago = int(s.draws[seat][street]), s.street-street
				break
			}
		}
		preferAggressor := seat == s.lastAggressor && best != s.lastAggressor
		if best < 0 || count < bestCount || count == bestCount && (ago < bestAgo || ago == bestAgo && (preferAggressor || best != s.lastAggressor && seat < best)) {
			best, bestCount, bestAgo = seat, count, ago
		}
	}
	return best, best >= 0
}

func (s *state) drawOrder(seat int) int {
	position := s.position(seat)
	if position == Button {
		return seatCount
	}
	return int(position)
}
