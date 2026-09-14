package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestFullSixSeatSessionFlushesLegalReplies(t *testing.T) {
	input := strings.Join([]string{
		`{"t":"hello","proto":1,"game_id":"27td-fl","stakes":{"kind":"blinds","small_blind":50,"big_blind":100},"betting":{"kind":"fixed-limit","raise_cap":4},"seat_count":6,"starting_stack":10000,"timeout_ms":1000}`,
		`{"t":"hand-start","hand_no":1,"seat":3}`,
		`{"t":"event","hand_no":1,"ev":{"event":"hand-start","button":0,"stacks":[10000,10000,10000,10000,10000,10000]}}`,
		`{"t":"event","hand_no":1,"ev":{"event":"deal-hole","seat":3,"cards":["2c","4d","7h","Qs","Kc"],"count":5}}`,
		`{"t":"event","hand_no":1,"ev":{"event":"street-start","street":0,"label":"predraw"}}`,
		`{"t":"act","hand_no":1,"seat":3,"decision":{"kind":"wager","fold":true,"call":100,"raise":{"min_to":200,"max_to":200}}}`,
		`{"t":"event","hand_no":1,"ev":{"event":"street-start","street":1,"label":"draw1"}}`,
		`{"t":"act","hand_no":1,"seat":3,"decision":{"kind":"draw","max_discards":5}}`,
		`{"t":"event","hand_no":1,"ev":{"event":"draw-result","seat":3,"discarded":["Qs","Kc"],"drawn":["3s","8c"],"count":2}}`,
		`{"t":"event","hand_no":1,"ev":{"event":"acted","seat":1,"action":{"kind":"bet","to":100},"street_commit":100}}`,
		`{"t":"act","hand_no":1,"seat":3,"decision":{"kind":"wager","fold":true,"call":100,"raise":{"min_to":200,"max_to":200}}}`,
		`{"t":"match-end"}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := run(strings.NewReader(input), &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("replies=%d output=%q", len(lines), output.String())
	}
	for i, line := range lines {
		var reply wire.BotMsg
		if err := json.Unmarshal([]byte(line), &reply); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if i == 0 && reply.Type != wire.MsgJoin {
			t.Fatalf("first=%+v", reply)
		}
		if i > 0 && (reply.Type != wire.MsgAction || reply.Action == nil) {
			t.Fatalf("action %d=%+v", i, reply)
		}
	}
}

func TestRejectsWrongSeatCount(t *testing.T) {
	err := run(strings.NewReader(`{"t":"hello","game_id":"27td-fl","seat_count":2}`+"\n"), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "six seats") {
		t.Fatalf("error=%v", err)
	}
}

func TestConfiguredBotProfiles(t *testing.T) {
	old := botProfile
	t.Cleanup(func() { botProfile = old })
	for _, profile := range []string{"baseline", "generation2"} {
		botProfile = profile
		if _, err := configuredBot(); err != nil {
			t.Fatalf("profile %s: %v", profile, err)
		}
	}
	botProfile = "unknown"
	if _, err := configuredBot(); err == nil {
		t.Fatal("unknown profile accepted")
	}
}
