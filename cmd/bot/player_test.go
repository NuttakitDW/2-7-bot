package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
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
