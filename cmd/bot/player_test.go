package main

import (
	"bytes"
	"fmt"
	"io"
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
	for _, profile := range []string{"learned", "learned-bayes"} {
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
