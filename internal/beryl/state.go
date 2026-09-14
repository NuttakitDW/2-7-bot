package beryl

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

const seatCount = 6

type Position uint8

const (
	Button Position = iota
	SmallBlind
	BigBlind
	UnderTheGun
	Hijack
	Cutoff
)

func (p Position) String() string {
	return [...]string{"button", "small-blind", "big-blind", "under-the-gun", "hijack", "cutoff"}[p]
}

type actionRecord struct {
	street int
	seat   int
	action string
	commit uint64
}

type state struct {
	seats, hero, button, street int
	bigBlind                    uint64
	firstOpener                 int
	predrawRaises, predrawCalls int
	heroPredrawRaises           int
	heroVoluntary               bool
	lastAggressor               int
	cards                       []cards.Card
	folded                      [seatCount]bool
	draws                       [seatCount][cfr.Streets]int8
	actions                     []actionRecord
	commitments                 [seatCount]uint64
	entryDecided, admitted      bool
}

func newState() *state {
	s := &state{}
	s.resetDraws()
	return s
}

func (s *state) hello(m wire.Message) { s.seats, s.bigBlind = m.SeatCount, m.Stakes.BigBlind }

func (s *state) handStart(m wire.Message) {
	seats, bigBlind := s.seats, s.bigBlind
	*s = state{seats: seats, bigBlind: bigBlind, hero: m.Seat, firstOpener: -1, lastAggressor: -1}
	s.resetDraws()
}

func (s *state) resetDraws() {
	for seat := range s.draws {
		for street := range s.draws[seat] {
			s.draws[seat][street] = -1
		}
	}
}

func (s *state) observe(e wire.Event) {
	switch e.Kind {
	case wire.EventHandStart:
		s.button = e.Button
	case wire.EventStreetStart:
		s.street = e.Street
	case wire.EventDealHole:
		if e.Seat == s.hero && len(e.Cards) > 0 {
			s.cards = cards.With(s.cards, e.Cards)
		}
	case wire.EventDrawResult:
		if validSeat(e.Seat) && s.street >= cfr.Draw1 && s.street <= cfr.Draw3 {
			s.draws[e.Seat][s.street] = int8(e.Count)
		}
		if e.Seat == s.hero {
			s.cards = cards.With(cards.Without(s.cards, e.Discarded), e.Drawn)
		}
	case wire.EventPost:
		if validSeat(e.Seat) && e.PostKind != "ante" {
			s.commitments[e.Seat] += e.Amount
		}
	case wire.EventActed:
		if !validSeat(e.Seat) {
			return
		}
		if e.Action.Kind == wire.ActionFold {
			s.folded[e.Seat] = true
		}
		if e.Action.Kind == wire.ActionBet || e.Action.Kind == wire.ActionRaise {
			s.lastAggressor = e.Seat
			if s.street == cfr.Predraw {
				s.predrawRaises++
				if s.firstOpener < 0 {
					s.firstOpener = e.Seat
				}
				if e.Seat == s.hero {
					s.heroPredrawRaises++
					s.heroVoluntary = true
				}
			}
		}
		if e.Action.Kind == wire.ActionCall && s.street == cfr.Predraw {
			s.predrawCalls++
			if e.Seat == s.hero {
				s.heroVoluntary = true
			}
		}
		s.commitments[e.Seat] = e.StreetCommit
		s.actions = append(s.actions, actionRecord{street: s.street, seat: e.Seat, action: e.Action.Kind, commit: e.StreetCommit})
	}
}

func validSeat(seat int) bool { return seat >= 0 && seat < seatCount }

func (s *state) position(seat int) Position {
	if !validSeat(seat) {
		return UnderTheGun
	}
	return Position((seat - s.button + seatCount) % seatCount)
}
