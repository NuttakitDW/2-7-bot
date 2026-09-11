package sixmax

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/sixmaxdata"
	"github.com/nuttakit/2-7-bot/internal/sixmaxrange"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// reachReport is deliberately test-only. It lets us measure a frozen range
// model against recorded public prefixes without adding data dependencies to
// the production bot.
type reachReport struct {
	Matches     map[string]*reachCounts `json:"matches"`
	Examples    []reachExample          `json:"examples"`
	Limitations []string                `json:"limitations"`
}

type reachCounts struct {
	Candidates       int            `json:"candidates"`
	Reasons          map[string]int `json:"reasons"`
	Baseline         map[string]int `json:"baseline"`
	Overlay          map[string]int `json:"overlay"`
	Changes          map[string]int `json:"changes"`
	Categories       map[string]int `json:"categories"`
	ChangeCategories map[string]int `json:"changeCategories"`
}

type reachExample struct {
	Match, Player, Position, Category, Observed, Baseline, Overlay, Reason string
	Hand, Event                                                            int
	Pot, Call                                                              int64
	Contexts                                                               []sixmaxrange.Context
}

type reachMatch struct {
	MatchInfo struct {
		ID      int      `json:"id"`
		Players []string `json:"players"`
	} `json:"matchInfo"`
}

func TestReplayPublicPrefixTracksClosingAction(t *testing.T) {
	obs := sixmaxdata.Observation{Player: 0, Street: "draw3", ActivePlayers: 3,
		Hand: []string{"2c", "3d", "4h", "7s", "9c"}, PotMilli: 5000, ToCallMilli: 1000,
		PublicActions: []sixmaxdata.PublicAction{
			{Player: 3, Street: "predraw", Action: "fold"},
			{Player: 4, Street: "predraw", Action: "fold"},
			{Player: 5, Street: "predraw", Action: "fold"},
			{Player: 1, Street: "draw3", Action: "check"},
			{Player: 2, Street: "draw3", Action: "bet", AmountMilli: 1000},
			{Player: 1, Street: "draw3", Action: "call", AmountMilli: 1000},
		}}
	for seat := range obs.DrawHistory {
		obs.DrawHistory[seat] = [3]int{-1, -1, -1}
	}
	obs.DrawHistory[0], obs.DrawHistory[1], obs.DrawHistory[2] = [3]int{1, 1, 1}, [3]int{1, 1, 0}, [3]int{1, 1, 1}
	s := replayPrefix(t, obs)
	if got := s.PlayersBehind(); got != 0 {
		t.Fatalf("players behind = %d, want closing action", got)
	}
	ctx, ok := s.RangeContext(1)
	if !ok || ctx.LastAction != wire.ActionCall || ctx.FinalDraw != sixmaxrange.DrawPat {
		t.Fatalf("caller context = %+v, known %t", ctx, ok)
	}
}

// TestH3HistoricalReachAnalysis is opt-in because it consumes ignored arena
// samples and the ignored fitted model. Set SIXMAX_REACH_ROOT to bin/sixmax.
func TestH3HistoricalReachAnalysis(t *testing.T) {
	root := os.Getenv("SIXMAX_REACH_ROOT")
	if root == "" {
		t.Skip("set SIXMAX_REACH_ROOT to run ignored-data reach analysis")
	}
	modelBytes, err := os.ReadFile(filepath.Join(root, "range-h3.json"))
	if err != nil {
		t.Fatal(err)
	}
	model, err := sixmaxrange.Decode(modelBytes)
	if err != nil {
		t.Fatal(err)
	}
	report := reachReport{Matches: map[string]*reachCounts{}, Limitations: []string{
		"derived public prefixes do not retain starting stacks or all-in flags; all-in and short-stack deferrals are unobservable and excluded from this replay",
		"derived decisions do not retain the offered raise range; replay approximates raise availability from the fixed-limit street aggression count, so counts measure conditional support reach rather than exact deployed eligibility or expected improvement",
	}}
	// 103 remains an untouched calibration holdout and is intentionally not
	// part of this development reach report.
	for _, id := range []int{36, 37, 38, 39, 41, 85, 101, 102, 1081, 1082} {
		analyseReachMatch(t, root, id, model, &report)
	}
	sort.Slice(report.Examples, func(i, j int) bool {
		if report.Examples[i].Match != report.Examples[j].Match {
			return report.Examples[i].Match < report.Examples[j].Match
		}
		if report.Examples[i].Hand != report.Examples[j].Hand {
			return report.Examples[i].Hand < report.Examples[j].Hand
		}
		return report.Examples[i].Event < report.Examples[j].Event
	})
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "reach-h3.json")
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", path)
}

func analyseReachMatch(t *testing.T, root string, id int, model *sixmaxrange.Model, report *reachReport) {
	t.Helper()
	dir := filepath.Join(root, "data", fmt.Sprintf("match-%d", id))
	var match reachMatch
	readJSON(t, filepath.Join(dir, "match.json"), &match)
	var hands []sixmaxdata.HandRecord
	readJSON(t, filepath.Join(dir, "derived.json"), &hands)
	for _, hand := range hands {
		for _, obs := range hand.Observations {
			if obs.Street != "draw3" || obs.ToCallMilli <= 0 {
				continue
			}
			player := "unknown"
			if obs.Player >= 0 && obs.Player < len(match.MatchInfo.Players) {
				player = match.MatchInfo.Players[obs.Player]
			}
			cohort := fmt.Sprintf("%d/all", id)
			analyseReachObservation(t, cohort, player, hand.HandNumber, obs, model, report)
			if strings.HasPrefix(player, "nutt-27td-fl-6max-h1") {
				analyseReachObservation(t, fmt.Sprintf("%d/our-h1", id), player, hand.HandNumber, obs, model, report)
			}
		}
	}
}

func analyseReachObservation(t *testing.T, cohort, player string, hand int, obs sixmaxdata.Observation, model *sixmaxrange.Model, report *reachReport) {
	t.Helper()
	c := report.Matches[cohort]
	if c == nil {
		c = &reachCounts{Reasons: map[string]int{}, Baseline: map[string]int{}, Overlay: map[string]int{}, Changes: map[string]int{}, Categories: map[string]int{}, ChangeCategories: map[string]int{}}
		report.Matches[cohort] = c
	}
	c.Candidates++
	s := replayPrefix(t, obs)
	bot := New(DefaultConfig())
	bot.State, bot.Ranges = s, model
	call := uint64(obs.ToCallMilli)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call}
	if obs.StreetAggression < 4 {
		d.Raise = &wire.Range{MinTo: call * 2, MaxTo: call * 2}
	}
	baseline := wire.Legalize(d, bot.propose(d), s.Hand.Cards)
	c.Baseline[baseline.Kind]++
	category := s.Hand.Category().String()
	c.Categories[category]++
	reason := rangeDeferReason(bot, d, baseline)
	overlay := baseline
	if reason == "supported" {
		var ok bool
		overlay, ok = bot.rangeCall(d, baseline)
		if !ok {
			t.Fatalf("classified supported but rangeCall deferred at match %s hand %d event %d", cohort, hand, obs.EventID)
		}
		c.Overlay[overlay.Kind]++
		if overlay.Kind != baseline.Kind {
			c.Changes[baseline.Kind+"->"+overlay.Kind]++
			c.ChangeCategories[category+":"+baseline.Kind+"->"+overlay.Kind]++
		}
	}
	c.Reasons[reason]++
	exampleGroup := reason
	if reason == "supported" && overlay.Kind != baseline.Kind {
		exampleGroup += "-change"
	}
	if exampleCount(report.Examples, cohort, exampleGroup) < 2 && (reason == "supported" || reason == "caller-unsupported" || reason == "players-pending") {
		contexts := make([]sixmaxrange.Context, 0, SeatCount-1)
		for seat := range s.Hand.Seats {
			if seat == s.Hand.Hero || s.Hand.Seats[seat].Folded {
				continue
			}
			if ctx, known := s.RangeContext(seat); known {
				contexts = append(contexts, ctx)
			}
		}
		report.Examples = append(report.Examples, reachExample{Match: cohort, Player: player, Position: obs.Position,
			Category: category, Observed: obs.Label.Action, Baseline: baseline.Kind, Overlay: overlay.Kind, Reason: reason,
			Hand: hand, Event: int(obs.EventID), Pot: obs.PotMilli, Call: obs.ToCallMilli, Contexts: contexts})
	}
}

func exampleCount(examples []reachExample, cohort, reason string) int {
	n := 0
	for _, example := range examples {
		exampleGroup := example.Reason
		if example.Reason == "supported" && example.Overlay != example.Baseline {
			exampleGroup += "-change"
		}
		if example.Match == cohort && exampleGroup == reason {
			n++
		}
	}
	return n
}

func rangeDeferReason(bot *Bot, d wire.Decision, baseline wire.Action) string {
	h := &bot.State.Hand
	if baseline.Kind == wire.ActionRaise {
		return "baseline-raise"
	}
	if bot.State.PlayersBehind() != 0 {
		return "players-pending"
	}
	if bot.State.HasAllIn() || h.SidePot {
		return "allin-or-sidepot"
	}
	for seat := range h.Seats {
		if seat == h.Hero || h.Seats[seat].Folded {
			continue
		}
		ctx, known := bot.State.RangeContext(seat)
		if !known {
			return "unknown-final-draw"
		}
		if _, supported := bot.Ranges.Lookup(ctx); !supported {
			if ctx.LastAction == wire.ActionCall {
				return "caller-unsupported"
			}
			return "range-unsupported:" + ctx.LastAction
		}
	}
	return "supported"
}

func replayPrefix(t *testing.T, obs sixmaxdata.Observation) *State {
	t.Helper()
	hand, err := cards.Parse(obs.Hand)
	if err != nil {
		t.Fatal(err)
	}
	s := NewState()
	s.HandStart(wire.Message{HandNo: 1, Seat: obs.Player})
	s.Observe(wire.Event{Kind: wire.EventHandStart, Stacks: []uint64{1 << 50, 1 << 50, 1 << 50, 1 << 50, 1 << 50, 1 << 50}})
	street := Predraw
	commits := [SeatCount]uint64{}
	for _, a := range obs.PublicActions {
		next := reachStreet(a.Street)
		if next != street {
			street = next
			commits = [SeatCount]uint64{}
			s.Observe(wire.Event{Kind: wire.EventStreetStart, Street: street, Label: a.Street})
		}
		if a.Action == "draw" {
			continue
		}
		if a.AmountMilli > 0 {
			commits[a.Player] += uint64(a.AmountMilli)
		}
		s.Observe(wire.Event{Kind: wire.EventActed, Seat: a.Player, Action: wire.Action{Kind: a.Action}, StreetCommit: commits[a.Player]})
	}
	if street != Draw3 {
		s.Observe(wire.Event{Kind: wire.EventStreetStart, Street: Draw3, Label: "draw3"})
	}
	for seat := range s.Hand.Seats {
		for draw := range 3 {
			s.Hand.Seats[seat].Draws[draw+Draw1] = obs.DrawHistory[seat][draw]
		}
	}
	s.Hand.Cards = hand
	s.Hand.Pot = uint64(obs.PotMilli)
	s.Hand.StreetAggressions = obs.StreetAggression
	return s
}

func reachStreet(label string) int {
	switch label {
	case "draw1":
		return Draw1
	case "draw2":
		return Draw2
	case "draw3":
		return Draw3
	default:
		return Predraw
	}
}

func readJSON(t *testing.T, path string, dst any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatal(err)
	}
}
