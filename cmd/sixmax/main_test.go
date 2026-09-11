package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/sixmaxclone"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestRunSixSeatWireSessionAndDrawFive(t *testing.T) {
	input := strings.Join([]string{
		`{"t":"hello","proto":1,"game_id":"27td-fl","stakes":{"kind":"blinds","small_blind":50,"big_blind":100,"ante":0},"betting":{"kind":"fixed-limit","raise_cap":4},"seat_count":6,"starting_stack":10000,"timeout_ms":1000}`,
		`{"t":"hand-start","hand_no":0,"seat":3}`,
		`{"t":"event","hand_no":0,"ev":{"event":"hand-start","hand_no":0,"button":0,"stacks":[10000,10000,10000,10000,10000,10000]}}`,
		`{"t":"event","hand_no":0,"ev":{"event":"street-start","street":1,"label":"draw1"}}`,
		`{"t":"event","hand_no":0,"ev":{"event":"deal-hole","seat":3,"cards":["Tc","Td","Jh","Qs","Ac"],"count":5}}`,
		`{"t":"act","hand_no":0,"seat":3,"decision":{"kind":"draw","max_discards":5},"deadline_ms":1000}`,
		`{"t":"match-end"}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := run(strings.NewReader(input), &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("reply lines = %d: %q", len(lines), output.String())
	}
	var join, action wire.BotMsg
	if err := json.Unmarshal([]byte(lines[0]), &join); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &action); err != nil {
		t.Fatal(err)
	}
	if join.Type != wire.MsgJoin || action.Action == nil || action.Action.Kind != wire.ActionDiscard {
		t.Fatalf("replies = %+v %+v", join, action)
	}
	held := cards.MustParse("Tc", "Td", "Jh", "Qs", "Ac")
	if got := wire.Legalize(wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5}, *action.Action, held); len(got.Cards) != 5 {
		t.Fatalf("draw-five exhaustion response = %+v", got)
	}
}

func TestRunRejectsNonSixSeatHello(t *testing.T) {
	input := `{"t":"hello","proto":1,"game_id":"27td-fl","seat_count":2}` + "\n"
	if err := run(strings.NewReader(input), &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "six seats") {
		t.Fatalf("run error = %v", err)
	}
}

func TestConfiguredBotKeepsCloneOffByDefault(t *testing.T) {
	restore := setCloneTestGlobals(t)
	defer restore()
	called := false
	loadCloneModel = func() (*sixmaxclone.Model, error) {
		called = true
		return nil, nil
	}
	bot, err := configuredBot()
	if err != nil {
		t.Fatal(err)
	}
	if called || bot.Clone != nil {
		t.Fatalf("default clone state: called=%t model=%v", called, bot.Clone)
	}
}

func TestConfiguredBotEnablesEmbeddedCloneAction(t *testing.T) {
	restore := setCloneTestGlobals(t)
	defer restore()
	clonePredraw = "true"
	leaf := &sixmaxclone.Leaf{Groups: 30, Probabilities: [3]float64{0, 0, 1}}
	model := &sixmaxclone.Model{Schema: sixmaxclone.SchemaVersion, MinGroups: 20, MinConfidence: .85,
		Trees: map[string]sixmaxclone.Tree{
			"facing":  {Nodes: []sixmaxclone.Node{{Leaf: leaf}}},
			"checked": {Nodes: []sixmaxclone.Node{{Leaf: leaf}}},
		}}
	loadCloneModel = func() (*sixmaxclone.Model, error) { return model, nil }
	bot, err := configuredBot()
	if err != nil {
		t.Fatal(err)
	}
	bot.State.Match.BigBlind = 100
	bot.State.Hand.Hero, bot.State.Hand.Button = 3, 0
	bot.State.Hand.Cards = cards.MustParse("3c", "5d", "8h", "Ks", "Ac")
	for seat := range bot.State.Hand.Seats {
		bot.State.Hand.Seats[seat].Stack = 10000
	}
	call := uint64(100)
	raise := wire.Range{MinTo: 200, MaxTo: 200}
	if got := bot.Decide(wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call, Raise: &raise}); got.Kind != wire.ActionRaise {
		t.Fatalf("enabled clone action = %s, want raise", got.Kind)
	}
}

func setCloneTestGlobals(t *testing.T) func() {
	t.Helper()
	oldValue, oldTen, oldRanges, oldClone := valueAggression, earlyTenBreak, rangeCalls, clonePredraw
	oldLoad := loadCloneModel
	valueAggression, earlyTenBreak, rangeCalls, clonePredraw = "false", "false", "false", "false"
	return func() {
		valueAggression, earlyTenBreak, rangeCalls, clonePredraw = oldValue, oldTen, oldRanges, oldClone
		loadCloneModel = oldLoad
	}
}
