package arena

import (
	"strings"
	"testing"
)

func TestCompetitionConfigValidate(t *testing.T) {
	valid := CompetitionConfig{
		Game:              "27td-fl",
		Players:           []string{"v1", "v2"},
		Hands:             10000,
		Duplicate:         true,
		CPUCores:          3,
		DecisionTimeoutMs: 5000,
	}
	if err := valid.Validate(false); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	tests := []struct {
		name    string
		isOFC   bool
		mutate  func(*CompetitionConfig)
		wantErr string
	}{
		{"no game", false, func(c *CompetitionConfig) { c.Game = "" }, "game is required"},
		{"one seat", false, func(c *CompetitionConfig) { c.Players = []string{"v1"} }, "seats 2 to 6"},
		{"seven seats", false, func(c *CompetitionConfig) {
			c.Players = []string{"a", "b", "c", "d", "e", "f", "g"}
			c.Hands = 10003
		}, "seats 2 to 6"},
		{"blank version id", false, func(c *CompetitionConfig) { c.Players[1] = "  " }, "seat 1 has no version id"},
		{"zero hands", false, func(c *CompetitionConfig) { c.Hands = 0 }, "hands must be between"},
		{"too many hands", false, func(c *CompetitionConfig) { c.Hands = MaxHands + 2 }, "hands must be between"},
		{"too many cores", false, func(c *CompetitionConfig) { c.CPUCores = 9 }, "cpuCores must be between"},
		{"timeout over cap", false, func(c *CompetitionConfig) { c.DecisionTimeoutMs = 5001 }, "decisionTimeoutMs must be between"},
		{"timeout zero", false, func(c *CompetitionConfig) { c.DecisionTimeoutMs = 0 }, "decisionTimeoutMs must be between"},
		// The rule that actually bites: duplicate dealing rotates every deck
		// through every seat, so hands must divide evenly.
		{"duplicate with indivisible hands", false, func(c *CompetitionConfig) { c.Hands = 10001 }, "divisible by 2 seats"},
		{"duplicate on OFC", true, func(c *CompetitionConfig) {
			c.Game = "ofc-pineapple"
			c.Players = []string{"v1", "v2"}
		}, "not available for OFC"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := valid
			config.Players = append([]string(nil), valid.Players...)
			test.mutate(&config)

			err := config.Validate(test.isOFC)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("error %q does not mention %q", err, test.wantErr)
			}
		})
	}
}

func TestSeededDealingAllowsAnyHandCount(t *testing.T) {
	config := CompetitionConfig{
		Game: "27td-fl", Players: []string{"v1", "v2", "v3"},
		Hands: 10001, Duplicate: false, CPUCores: 1, DecisionTimeoutMs: 1000,
	}
	if err := config.Validate(false); err != nil {
		t.Errorf("the divisibility rule applies only to duplicate dealing: %v", err)
	}
}

func TestIsOFC(t *testing.T) {
	ofc := []string{"ofc", "ofc-pineapple", "ofc-progressive", "ofc-27"}
	betting := []string{"27td-fl", "holdem-nl", "badugi-fl", "drawmaha-27-fl"}

	for _, game := range ofc {
		if !IsOFC(game) {
			t.Errorf("%s should be OFC", game)
		}
	}
	for _, game := range betting {
		if IsOFC(game) {
			t.Errorf("%s should not be OFC", game)
		}
	}
}

func TestCompetitionDone(t *testing.T) {
	for _, state := range []string{"completed", "failed", "cancelled"} {
		if !(Competition{State: state}).Done() {
			t.Errorf("%s should be terminal", state)
		}
	}
	for _, state := range []string{"queued", "provisioning", "running"} {
		if (Competition{State: state}).Done() {
			t.Errorf("%s should not be terminal", state)
		}
	}
}
