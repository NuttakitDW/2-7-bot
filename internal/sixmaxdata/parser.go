// Package sixmaxdata collects and derives leak-free six-player arena samples.
package sixmaxdata

import (
	"fmt"
	"slices"
	"strings"

	"github.com/nuttakit/2-7-bot/internal/arena"
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
)

var positions = [...]string{"SB", "BB", "UTG", "HJ", "CO", "BTN"}

type Seat struct {
	Player   int    `json:"player"`
	Position string `json:"position"`
}

type PublicAction struct {
	Player      int    `json:"player"`
	Position    string `json:"position"`
	Street      string `json:"street"`
	Action      string `json:"action"`
	AmountMilli int64  `json:"amountMilli"`
}

type Label struct {
	Action      string   `json:"action"`
	AmountMilli int64    `json:"amountMilli,omitempty"`
	DrawCount   int      `json:"drawCount"`
	Discard     []string `json:"discard,omitempty"`
}

// Observation contains only the public event prefix and the acting player's
// current hand. The current event is represented exclusively by Label.
type Observation struct {
	EventID          int64          `json:"eventId"`
	Player           int            `json:"player"`
	Position         string         `json:"position"`
	Street           string         `json:"street"`
	Hand             []string       `json:"hand"`
	PotMilli         int64          `json:"potMilli"`
	DeadPotMilli     int64          `json:"deadPotMilli"`
	ToCallMilli      int64          `json:"toCallMilli"`
	ActivePlayers    int            `json:"activePlayers"`
	StreetAggression int            `json:"streetAggression"`
	DrawHistory      [6][3]int      `json:"drawHistory"`
	PublicActions    []PublicAction `json:"publicActions"`
	Label            Label          `json:"label"`
}

type FinalDrawRank struct {
	Player   int         `json:"player"`
	Position string      `json:"position"`
	Value    deuce.Value `json:"value"`
	Class    string      `json:"class"`
}

type HandRecord struct {
	MatchID        int             `json:"matchId"`
	HandNumber     int             `json:"handNumber"`
	SplitKey       string          `json:"splitKey"`
	Seats          [6]Seat         `json:"seats"`
	Observations   []Observation   `json:"observations"`
	FinalDrawRanks []FinalDrawRank `json:"finalDrawRanks"`
}

// ParseHand derives training observations from a hosted hand log. Arena
// player numbers are bot indexes. Positions come from initial-cards deal
// order, which is SB, BB, UTG, HJ, CO, BTN and is intentionally not sorted.
func ParseHand(matchID int, dealMode string, detail arena.HandDetail) (HandRecord, error) {
	record := HandRecord{MatchID: matchID, HandNumber: detail.Hand.Number}
	group := detail.Hand.Number
	if dealMode == "duplicate" {
		group = (detail.Hand.Number-1)/6 + 1
	}
	record.SplitKey = fmt.Sprintf("%d:%d", matchID, group)

	var hands [6][]cards.Card
	var positionByPlayer [6]string
	var seenInitial [6]bool
	var folded [6]bool
	var reachedFinal [6]bool
	var drawHistory [6][3]int
	for p := range drawHistory {
		for d := range drawHistory[p] {
			drawHistory[p][d] = -1
		}
	}
	var streetCommit [6]int64
	var totalCommit [6]int64
	var pot int64
	var public []PublicAction
	street, aggression := "predraw", 0
	dealIndex := 0

	for _, event := range detail.Events {
		if event.Player == nil || *event.Player < 0 || *event.Player >= 6 {
			continue
		}
		p := *event.Player
		switch event.Kind {
		case "initial-cards":
			if seenInitial[p] {
				return record, fmt.Errorf("event %d: duplicate initial cards for player %d", event.ID, p)
			}
			if dealIndex >= len(positions) {
				return record, fmt.Errorf("event %d: more than six initial hands", event.ID)
			}
			hand, err := parsePacked(value(event.Cards))
			if err != nil || len(hand) != 5 || cards.NewSet(hand).Len() != 5 {
				return record, fmt.Errorf("event %d: invalid initial cards: %w", event.ID, err)
			}
			hands[p], seenInitial[p] = hand, true
			positionByPlayer[p] = positions[dealIndex]
			record.Seats[p] = Seat{Player: p, Position: positions[dealIndex]}
			dealIndex++
		case "forced":
			amount := amountValue(event.AmountMilli)
			pot += amount
			streetCommit[p] += amount
			totalCommit[p] += amount
		case "action", "replacement":
			if !seenInitial[p] {
				return record, fmt.Errorf("event %d: player %d acts before initial cards", event.ID, p)
			}
			eventStreet := value(event.Street)
			if eventStreet == "" {
				return record, fmt.Errorf("event %d: missing street", event.ID)
			}
			if eventStreet != street {
				street = eventStreet
				streetCommit = [6]int64{}
				aggression = 0
			}
			active := 0
			for i := range folded {
				if seenInitial[i] && !folded[i] {
					active++
				}
			}
			maxCommit := int64(0)
			deadPot := int64(0)
			for i, committed := range streetCommit {
				if folded[i] {
					deadPot += totalCommit[i]
					continue
				}
				maxCommit = max(maxCommit, committed)
			}
			obs := Observation{
				EventID: event.ID, Player: p, Position: positionByPlayer[p], Street: street,
				Hand: cardStrings(hands[p]), PotMilli: pot, DeadPotMilli: deadPot, ToCallMilli: max(0, maxCommit-streetCommit[p]),
				ActivePlayers: active, StreetAggression: aggression, DrawHistory: drawHistory,
				PublicActions: slices.Clone(public),
			}
			if event.Kind == "action" {
				if event.Action == nil {
					return record, fmt.Errorf("event %d: missing action", event.ID)
				}
				amount := amountValue(event.AmountMilli)
				obs.Label = Label{Action: *event.Action, AmountMilli: amount}
				record.Observations = append(record.Observations, obs)
				public = append(public, PublicAction{Player: p, Position: positionByPlayer[p], Street: street, Action: *event.Action, AmountMilli: amount})
				pot += amount
				streetCommit[p] += amount
				totalCommit[p] += amount
				if *event.Action == "bet" || *event.Action == "raise" {
					aggression++
				}
				if *event.Action == "fold" {
					folded[p] = true
				}
				continue
			}

			incoming, err := parsePacked(value(event.Cards))
			if err != nil {
				return record, fmt.Errorf("event %d: replacement cards: %w", event.ID, err)
			}
			discard, err := parsePacked(value(event.Detail))
			if err != nil {
				return record, fmt.Errorf("event %d: discarded cards: %w", event.ID, err)
			}
			if len(incoming) != len(discard) {
				return record, fmt.Errorf("event %d: unbalanced replacement", event.ID)
			}
			obs.Label = Label{Action: "draw", DrawCount: len(incoming), Discard: cardStrings(discard)}
			record.Observations = append(record.Observations, obs)
			next, err := replace(hands[p], discard, incoming)
			if err != nil {
				return record, fmt.Errorf("event %d: %w", event.ID, err)
			}
			hands[p] = next
			if d := drawIndex(street); d >= 0 {
				drawHistory[p][d] = len(incoming)
			}
			public = append(public, PublicAction{Player: p, Position: positionByPlayer[p], Street: street, Action: "draw", AmountMilli: int64(len(incoming))})
			if street == "draw3" {
				reachedFinal[p] = true
			}
		}
	}

	for p, reached := range reachedFinal {
		if !reached {
			continue
		}
		if len(hands[p]) != 5 || cards.NewSet(hands[p]).Len() != 5 {
			return record, fmt.Errorf("player %d has invalid final hand", p)
		}
		v := deuce.Eval(hands[p])
		record.FinalDrawRanks = append(record.FinalDrawRanks, FinalDrawRank{Player: p, Position: positionByPlayer[p], Value: v, Class: v.Class().String()})
	}
	if dealIndex != len(positions) {
		return record, fmt.Errorf("hand has %d initial hands, want 6", dealIndex)
	}
	return record, nil
}

func drawIndex(street string) int {
	switch street {
	case "draw1":
		return 0
	case "draw2":
		return 1
	case "draw3":
		return 2
	}
	return -1
}

func value(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func amountValue(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func parsePacked(text string) ([]cards.Card, error) {
	if len(text)%2 != 0 {
		return nil, fmt.Errorf("odd packed card string %q", text)
	}
	out := make([]cards.Card, 0, len(text)/2)
	for i := 0; i < len(text); i += 2 {
		card, err := cards.ParseCard(text[i : i+2])
		if err != nil {
			return nil, err
		}
		out = append(out, card)
	}
	return out, nil
}

func cardStrings(hand []cards.Card) []string {
	out := make([]string, len(hand))
	for i, card := range hand {
		out[i] = card.String()
	}
	return out
}

func replace(hand, discard, incoming []cards.Card) ([]cards.Card, error) {
	next := slices.Clone(hand)
	for i, old := range discard {
		found := -1
		for j, held := range next {
			if held == old {
				found = j
				break
			}
		}
		if found < 0 {
			return nil, fmt.Errorf("discard %s is not held", old)
		}
		next[found] = incoming[i]
	}
	if len(next) != 5 || cards.NewSet(next).Len() != 5 {
		return nil, fmt.Errorf("replacement creates invalid hand %s", strings.Join(cardStrings(next), ""))
	}
	return next, nil
}
