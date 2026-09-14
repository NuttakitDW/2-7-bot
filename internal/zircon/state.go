// Package zircon implements a dedicated six-seat 2-7 triple-draw strategy.
package zircon

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
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
	GameID, BettingKind                       string
	SeatCount                                 int
	StartingStack, SmallBlind, BigBlind, Ante uint64
	RaiseCap                                  int
	TimeoutMs                                 uint64
}

type SeatState struct {
	Stack, Contribution, StreetCommit uint64
	Folded, AllIn, ActedOnStreet      bool
	Draws                             [Streets]int
}

type ActionRecord struct {
	Street, Seat int
	Action       wire.Action
	Commit       uint64
}

type Hand struct {
	No                               uint64
	Hero, Button, Street             int
	Label                            string
	Cards                            []cards.Card
	Pot                              uint64
	Seats                            [SeatCount]SeatState
	Actions                          []ActionRecord
	StreetAggressions, StreetCalls   int
	LastAggressor, HeroPredrawRaises int
	SidePot                          bool
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
	s.Match = Match{GameID: m.GameID, BettingKind: m.Betting.Kind, SeatCount: m.SeatCount,
		StartingStack: m.StartingStack, SmallBlind: m.Stakes.SmallBlind, BigBlind: m.Stakes.BigBlind, Ante: m.Stakes.Ante}
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
			seat.StreetCommit = e.StreetCommit
		}
		seat.AllIn = seat.AllIn || e.AllIn
		if e.StreetCommit > previousHigh && (e.Action.Kind == wire.ActionBet || e.Action.Kind == wire.ActionRaise) {
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
		h.updateSidePot()
	}
}

func validSeat(seat int) bool { return seat >= 0 && seat < SeatCount }

func (h *Hand) updateSidePot() {
	max := uint64(0)
	for i := range h.Seats {
		if !h.Seats[i].Folded && h.Seats[i].Contribution > max {
			max = h.Seats[i].Contribution
		}
	}
	for i := range h.Seats {
		if !h.Seats[i].Folded && h.Seats[i].AllIn && h.Seats[i].Contribution < max {
			h.SidePot = true
			return
		}
	}
}

func (s *State) Position(seat int) Position {
	if !validSeat(seat) {
		return UnderTheGun
	}
	return Position((seat - s.Hand.Button + SeatCount) % SeatCount)
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
	Seat, Count, StreetsAgo int
	Known                   bool
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
		if seat != s.Hand.Hero && !s.Hand.Seats[seat].Folded && !s.Hand.Seats[seat].AllIn && !s.Hand.Seats[seat].ActedOnStreet {
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

func (s *State) CurrentLiveDraws() ([]int, bool) {
	draws := make([]int, 0, SeatCount-1)
	for seat := range s.Hand.Seats {
		if seat == s.Hand.Hero || s.Hand.Seats[seat].Folded {
			continue
		}
		count := s.Hand.Seats[seat].Draws[s.Hand.Street]
		if count < 0 {
			return nil, false
		}
		draws = append(draws, count)
	}
	return draws, true
}
