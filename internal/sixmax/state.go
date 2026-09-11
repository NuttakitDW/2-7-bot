// Package sixmax implements the six-seat 2-7 triple-draw bot. It owns its
// ring-game state instead of inheriting heads-up assumptions from table/onyx.
package sixmax

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/sixmaxrange"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

const SeatCount = 6

const (
	Predraw = iota
	Draw1
	Draw2
	Draw3
	Streets
)

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

type Match struct {
	GameID        string
	SeatCount     int
	StartingStack uint64
	SmallBlind    uint64
	BigBlind      uint64
	Ante          uint64
	RaiseCap      int
	TimeoutMs     uint64
}

type SeatState struct {
	Stack         uint64
	Folded        bool
	AllIn         bool
	Contribution  uint64
	StreetCommit  uint64
	Draws         [Streets]int
	ActedOnStreet bool
}

type ActionRecord struct {
	Street int
	Seat   int
	Action wire.Action
	Commit uint64
}

type Hand struct {
	No                uint64
	Hero              int
	Button            int
	Street            int
	Label             string
	Cards             []cards.Card
	Pot               uint64
	Seats             [SeatCount]SeatState
	Actions           []ActionRecord
	StreetAggressions int
	StreetCalls       int
	LastAggressor     int
	HeroPredrawRaises int
	SidePot           bool
}

func (h *Hand) Complete() bool { return len(h.Cards) == deuce.HandSize }
func (h *Hand) Category() deuce.Category {
	if !h.Complete() {
		return deuce.Broken
	}
	return deuce.Categorize(h.Cards)
}

type State struct {
	Match Match
	Hand  Hand
}

func NewState() *State { return &State{} }

func (s *State) Hello(m wire.Message) {
	s.Match = Match{GameID: m.GameID, SeatCount: m.SeatCount, StartingStack: m.StartingStack,
		SmallBlind: m.Stakes.SmallBlind, BigBlind: m.Stakes.BigBlind, Ante: m.Stakes.Ante}
	if m.Betting.RaiseCap != nil {
		s.Match.RaiseCap = int(*m.Betting.RaiseCap)
	}
	if m.TimeoutMs != nil {
		s.Match.TimeoutMs = *m.TimeoutMs
	}
}

func (s *State) HandStart(m wire.Message) {
	s.Hand = Hand{No: m.HandNo, Hero: m.Seat, Street: Predraw, Label: "predraw", LastAggressor: -1}
	for seat := range s.Hand.Seats {
		for street := range s.Hand.Seats[seat].Draws {
			s.Hand.Seats[seat].Draws[street] = -1
		}
	}
}

func (s *State) Observe(e wire.Event) {
	h := &s.Hand
	switch e.Kind {
	case wire.EventHandStart:
		h.Button = e.Button
		for i, stack := range e.Stacks {
			if i < SeatCount {
				h.Seats[i].Stack = stack
			}
		}
	case wire.EventPost:
		if !validSeat(e.Seat) {
			return
		}
		h.Pot += e.Amount
		h.Seats[e.Seat].Contribution += e.Amount
		if e.PostKind != "ante" {
			h.Seats[e.Seat].StreetCommit += e.Amount
		}
		h.Seats[e.Seat].AllIn = h.Seats[e.Seat].AllIn || e.AllIn
		h.updateSidePot()
	case wire.EventStreetStart:
		h.Street, h.Label = e.Street, e.Label
		if e.Street != Predraw {
			for i := range h.Seats {
				h.Seats[i].StreetCommit = 0
				h.Seats[i].ActedOnStreet = false
			}
			h.StreetAggressions, h.StreetCalls, h.LastAggressor = 0, 0, -1
		}
	case wire.EventDealHole:
		if e.Seat == h.Hero && len(e.Cards) > 0 {
			h.Cards = cards.With(h.Cards, e.Cards)
		}
	case wire.EventDrawResult:
		if validSeat(e.Seat) && h.Street >= Draw1 && h.Street <= Draw3 {
			h.Seats[e.Seat].Draws[h.Street] = e.Count
			h.Seats[e.Seat].AllIn = h.Seats[e.Seat].AllIn || e.AllIn
		}
		if e.Seat == h.Hero {
			h.Cards = cards.With(cards.Without(h.Cards, e.Discarded), e.Drawn)
		}
	case wire.EventActed:
		if !validSeat(e.Seat) {
			return
		}
		previousHigh := uint64(0)
		for i := range h.Seats {
			if h.Seats[i].StreetCommit > previousHigh {
				previousHigh = h.Seats[i].StreetCommit
			}
		}
		seat := &h.Seats[e.Seat]
		if e.StreetCommit > seat.StreetCommit {
			delta := e.StreetCommit - seat.StreetCommit
			h.Pot += delta
			seat.Contribution += delta
		}
		if e.StreetCommit >= seat.StreetCommit {
			seat.StreetCommit = e.StreetCommit
		}
		seat.AllIn = seat.AllIn || e.AllIn
		h.updateSidePot()
		// A new high commitment gives every eligible player another
		// decision, including seats that checked before the bet. This also
		// covers a short all-in raise: the wire commitment is authoritative
		// even where it is smaller than a normal fixed-limit increment.
		if e.StreetCommit > previousHigh &&
			(e.Action.Kind == wire.ActionBet || e.Action.Kind == wire.ActionRaise) {
			for i := range h.Seats {
				if i != e.Seat && !h.Seats[i].Folded && !h.Seats[i].AllIn {
					h.Seats[i].ActedOnStreet = false
				}
			}
		}
		seat.ActedOnStreet = true
		switch e.Action.Kind {
		case wire.ActionFold:
			seat.Folded = true
		case wire.ActionBet, wire.ActionRaise:
			h.StreetAggressions++
			h.LastAggressor = e.Seat
			if h.Street == Predraw && e.Seat == h.Hero && e.Action.Kind == wire.ActionRaise {
				h.HeroPredrawRaises++
			}
		case wire.ActionCall:
			h.StreetCalls++
		}
		h.Actions = append(h.Actions, ActionRecord{Street: h.Street, Seat: e.Seat, Action: e.Action, Commit: e.StreetCommit})
	}
}

func validSeat(seat int) bool { return seat >= 0 && seat < SeatCount }

func (h *Hand) updateSidePot() {
	maximum := uint64(0)
	for seat := range h.Seats {
		if !h.Seats[seat].Folded && h.Seats[seat].Contribution > maximum {
			maximum = h.Seats[seat].Contribution
		}
	}
	for seat := range h.Seats {
		if !h.Seats[seat].Folded && h.Seats[seat].AllIn && h.Seats[seat].Contribution < maximum {
			h.SidePot = true
			return
		}
	}
}

func (s *State) Position(seat int) Position {
	if !validSeat(seat) {
		return UnderTheGun
	}
	distance := (seat - s.Hand.Button + SeatCount) % SeatCount
	return Position(distance)
}

func (s *State) LiveOpponents() int {
	n := 0
	for seat := range s.Hand.Seats {
		if seat != s.Hand.Hero && !s.Hand.Seats[seat].Folded {
			n++
		}
	}
	return n
}

type DrawRead struct {
	Seat       int
	Count      int
	StreetsAgo int
	Known      bool
}

func (s *State) LatestDraw(seat int) (DrawRead, bool) {
	if !validSeat(seat) {
		return DrawRead{}, false
	}
	for street := min(s.Hand.Street, Draw3); street >= Draw1; street-- {
		if count := s.Hand.Seats[seat].Draws[street]; count >= 0 {
			return DrawRead{Seat: seat, Count: count, StreetsAgo: s.Hand.Street - street, Known: true}, true
		}
	}
	return DrawRead{Seat: seat}, false
}

func (s *State) LiveOpponentDraws() []DrawRead {
	reads := make([]DrawRead, 0, SeatCount-1)
	for seat := range s.Hand.Seats {
		if seat == s.Hand.Hero || s.Hand.Seats[seat].Folded {
			continue
		}
		read, ok := s.LatestDraw(seat)
		read.Known = ok
		reads = append(reads, read)
	}
	return reads
}

func (s *State) PlayersBehind() int {
	n := 0
	for seat := range s.Hand.Seats {
		if seat == s.Hand.Hero || s.Hand.Seats[seat].Folded || s.Hand.Seats[seat].AllIn {
			continue
		}
		if !s.Hand.Seats[seat].ActedOnStreet {
			n++
		}
	}
	return n
}

func (s *State) HasAllIn() bool {
	for seat := range s.Hand.Seats {
		if s.Hand.Seats[seat].AllIn {
			return true
		}
	}
	return false
}

// RangeContext builds the same public prefix features used by the offline
// fitter. It deliberately has no access to any opponent cards.
func (s *State) RangeContext(target int) (sixmaxrange.Context, bool) {
	var draws [SeatCount][3]int
	for seat := range s.Hand.Seats {
		for i := range 3 {
			draws[seat][i] = s.Hand.Seats[seat].Draws[i+Draw1]
		}
	}
	actions := make([]sixmaxrange.PublicAction, 0, len(s.Hand.Actions))
	for _, action := range s.Hand.Actions {
		actions = append(actions, sixmaxrange.PublicAction{Seat: action.Seat, Street: action.Street, Action: action.Action.Kind})
	}
	return sixmaxrange.BuildContext(target, draws, actions, s.LiveOpponents()+1)
}
