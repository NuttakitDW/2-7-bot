package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestModelBettingProfileRunsWireSession(t *testing.T) {
	old := playerProfile
	playerProfile = "model-bets"
	t.Cleanup(func() { playerProfile = old })
	for hero := 0; hero < 2; hero++ {
		t.Run(fmt.Sprint(hero), func(t *testing.T) { playProtocolSession(t, hero) })
	}
}

func TestLearnedProfilesRunWireSession(t *testing.T) {
	old := playerProfile
	t.Cleanup(func() { playerProfile = old })
	for _, profile := range []string{"learned", "learned-bayes", "azurite"} {
		playerProfile = profile
		for hero := 0; hero < 2; hero++ {
			t.Run(fmt.Sprint(profile, hero), func(t *testing.T) { playProtocolSession(t, hero) })
		}
	}
}

func TestUnknownPlayerProfileFailsBeforeJoining(t *testing.T) {
	old := playerProfile
	playerProfile = "typo"
	t.Cleanup(func() { playerProfile = old })
	var replies bytes.Buffer
	if err := run(strings.NewReader(session), &replies, io.Discard); err == nil {
		t.Fatal("accepted unknown player")
	}
	if replies.Len() != 0 {
		t.Fatal("joined with invalid profile")
	}
}

func TestEmpiricalProfileRejectsBlueprintBeforeJoining(t *testing.T) {
	old := playerProfile
	playerProfile = "empirical"
	t.Cleanup(func() { playerProfile = old })
	var replies bytes.Buffer
	if err := run(strings.NewReader(session), &replies, io.Discard); err == nil {
		t.Fatal("accepted blueprint as empirical JSON")
	}
	if replies.Len() != 0 {
		t.Fatal("joined with invalid empirical artifact")
	}
}

// Run with the empirical JSON build overlay to exercise the actual artifact.
func TestEmpiricalProfileRunsWireSession(t *testing.T) {
	if os.Getenv("EMPIRICAL_PROFILE_TEST") == "" {
		t.Skip("no empirical build overlay supplied")
	}
	old := playerProfile
	playerProfile = "empirical"
	t.Cleanup(func() { playerProfile = old })
	for hero := 0; hero < 2; hero++ {
		t.Run(fmt.Sprint(hero), func(t *testing.T) { playProtocolSession(t, hero) })
	}
}

func TestRiverResponseProfileRejectsBlueprintBeforeJoining(t *testing.T) {
	old := playerProfile
	playerProfile = "river-response"
	t.Cleanup(func() { playerProfile = old })
	var replies bytes.Buffer
	if err := run(strings.NewReader(session), &replies, io.Discard); err == nil {
		t.Fatal("accepted incompatible response payload")
	}
	if replies.Len() != 0 {
		t.Fatal("joined with invalid response artifact")
	}
}

func TestBlockerRiverResponseProfileRejectsBlueprintBeforeJoining(t *testing.T) {
	old := playerProfile
	playerProfile = "river-response-blockers"
	t.Cleanup(func() { playerProfile = old })
	var replies bytes.Buffer
	if err := run(strings.NewReader(session), &replies, io.Discard); err == nil {
		t.Fatal("accepted incompatible blocker response payload")
	}
	if replies.Len() != 0 {
		t.Fatal("joined with invalid blocker response artifact")
	}
}

func TestRiverResponseProfileRunsWireSession(t *testing.T) {
	if os.Getenv("RIVER_RESPONSE_PROFILE_TEST") == "" {
		t.Skip("no river response build overlay supplied")
	}
	old := playerProfile
	playerProfile = "river-response"
	t.Cleanup(func() { playerProfile = old })
	for hero := 0; hero < 2; hero++ {
		t.Run(fmt.Sprint(hero), func(t *testing.T) { playProtocolSession(t, hero) })
	}
}

func TestBlockerRiverResponseProfileRunsWireSession(t *testing.T) {
	if os.Getenv("RIVER_RESPONSE_PROFILE_TEST") == "" {
		t.Skip("no river response build overlay supplied")
	}
	old := playerProfile
	playerProfile = "river-response-blockers"
	t.Cleanup(func() { playerProfile = old })
	for hero := 0; hero < 2; hero++ {
		t.Run(fmt.Sprint(hero), func(t *testing.T) { playProtocolSession(t, hero) })
	}
}

// Spinel's learned tracker has a heads-up tree. A six-seat hand deliberately
// routes every decision to the unchanged Onyx fallback; exercise that wire path
// from every possible hero seat with the real overlaid Spinel policy.
func TestBlockerRiverResponseProfileRunsSixMaxFallbackAtEverySeat(t *testing.T) {
	if os.Getenv("RIVER_RESPONSE_PROFILE_TEST") == "" {
		t.Skip("no river response build overlay supplied")
	}
	old := playerProfile
	playerProfile = "river-response-blockers"
	t.Cleanup(func() { playerProfile = old })

	for hero := 0; hero < 6; hero++ {
		t.Run(fmt.Sprint(hero), func(t *testing.T) {
			input := fmt.Sprintf(`
{"t":"hello","proto":1,"game_id":"27td-fl","stakes":{"kind":"blinds","small_blind":50,"big_blind":100,"ante":0},"betting":{"kind":"fixed-limit","raise_cap":4},"seat_count":6,"starting_stack":10000,"timeout_ms":1000}
{"t":"hand-start","hand_no":0,"seat":%d}
{"t":"event","hand_no":0,"ev":{"event":"hand-start","hand_no":0,"button":0,"stacks":[10000,10000,10000,10000,10000,10000]}}
{"t":"event","hand_no":0,"ev":{"event":"deal-hole","seat":%d,"cards":["7c","5d","4h","3s","2c"],"count":5}}
{"t":"act","hand_no":0,"seat":%d,"decision":{"kind":"draw","max_discards":5},"deadline_ms":1000}
{"t":"match-end"}
`, hero, hero, hero)

			var replies, debug bytes.Buffer
			if err := run(strings.NewReader(input), &replies, &debug); err != nil {
				t.Fatalf("run: %v", err)
			}
			lines := strings.Split(strings.TrimSpace(replies.String()), "\n")
			if len(lines) != 2 {
				t.Fatalf("replies = %q, want join and one action", replies.String())
			}
			var reply wire.BotMsg
			if err := json.Unmarshal([]byte(lines[1]), &reply); err != nil {
				t.Fatalf("decode action: %v", err)
			}
			if reply.Type != wire.MsgAction || reply.Action == nil || reply.Action.Kind != wire.ActionDiscard {
				t.Fatalf("reply = %+v, want a legal draw action", reply)
			}
			if !strings.Contains(debug.String(), "1 blueprint fallbacks") {
				t.Errorf("debug = %q, want proof the six-seat decision used Onyx", debug.String())
			}
		})
	}
}
