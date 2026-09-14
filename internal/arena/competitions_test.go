package arena

import (
	"io"
	"net/http"
	"net/http/httptest"
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

func TestCreateCompetitionRejectsExcludedLatestVersionBeforePOST(t *testing.T) {
	posts := 0
	historyRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/bots":
			_, _ = io.WriteString(w, `[{"id":"paul","name":"Paul-Sauron-301","latestVersion":{"id":"paul-latest"}},{"id":"good","name":"Gandalf","latestVersion":{"id":"good-latest"}}]`)
		case r.Method == http.MethodGet:
			historyRequests++
			http.Error(w, "unexpected history request", http.StatusInternalServerError)
		case r.Method == http.MethodPost:
			posts++
			_, _ = io.WriteString(w, `{"id":"created"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := New(server.URL, "key").CreateCompetition(t.Context(), competitionConfig("paul-latest", "good-latest"))
	if err == nil || !strings.Contains(err.Error(), "Paul-Sauron-301") {
		t.Fatalf("expected local exclusion error, got %v", err)
	}
	if posts != 0 || historyRequests != 0 {
		t.Fatalf("posts = %d, history requests = %d; want both zero", posts, historyRequests)
	}
}

func TestCreateCompetitionRejectsExcludedHistoricalVersionBeforePOST(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/bots":
			_, _ = io.WriteString(w, `[{"id":"paul","name":" paul-sauron-old ","latestVersion":{"id":"paul-latest"}},{"id":"good","name":"Gandalf","latestVersion":{"id":"good-latest"}}]`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/bots/paul/versions":
			_, _ = io.WriteString(w, `[{"id":"paul-old"},{"id":"paul-latest"}]`)
		case r.Method == http.MethodPost:
			posts++
			_, _ = io.WriteString(w, `{"id":"created"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := New(server.URL, "key").CreateCompetition(t.Context(), competitionConfig("paul-old", "good-latest"))
	if err == nil || !strings.Contains(err.Error(), "paul-sauron-old") {
		t.Fatalf("expected historical version exclusion error, got %v", err)
	}
	if posts != 0 {
		t.Fatalf("competition POST count = %d, want 0", posts)
	}
}

func TestCreateCompetitionPostsAllowedRoster(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/bots":
			_, _ = io.WriteString(w, `[{"id":"one","name":"Gandalf","latestVersion":{"id":"v1"}},{"id":"two","name":"Paul-Atreides","latestVersion":{"id":"v2"}}]`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/competitions":
			posts++
			_, _ = io.WriteString(w, `{"id":"created"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	created, err := New(server.URL, "key").CreateCompetition(t.Context(), competitionConfig("v1", "v2"))
	if err != nil {
		t.Fatalf("allowed competition rejected: %v", err)
	}
	if created.ID != "created" || posts != 1 {
		t.Fatalf("created = %#v, posts = %d", created, posts)
	}
}

func TestCreateCompetitionAllowsUnknownVersionAfterExcludedHistoriesChecked(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/bots":
			_, _ = io.WriteString(w, `[{"id":"paul","name":"Paul-Sauron","latestVersion":{"id":"paul-latest"}},{"id":"good","name":"Gandalf","latestVersion":{"id":"good-latest"}}]`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/bots/paul/versions":
			_, _ = io.WriteString(w, `[{"id":"paul-old"},{"id":"paul-latest"}]`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/competitions":
			posts++
			_, _ = io.WriteString(w, `{"id":"created"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := New(server.URL, "key").CreateCompetition(t.Context(), competitionConfig("allowed-old", "good-latest"))
	if err != nil {
		t.Fatalf("old allowed version rejected: %v", err)
	}
	if posts != 1 {
		t.Fatalf("competition POST count = %d, want 1", posts)
	}
}

func TestCreateCompetitionLookupFailuresPreventPOST(t *testing.T) {
	tests := []struct {
		name       string
		botsStatus int
	}{
		{"roster lookup", http.StatusServiceUnavailable},
		{"excluded history lookup", http.StatusOK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/bots" && test.botsStatus != http.StatusOK:
					http.Error(w, "roster unavailable", test.botsStatus)
				case r.Method == http.MethodGet && r.URL.Path == "/api/bots":
					_, _ = io.WriteString(w, `[{"id":"paul","name":"Paul-Sauron","latestVersion":{"id":"paul-latest"}},{"id":"good","name":"Gandalf","latestVersion":{"id":"good-latest"}}]`)
				case r.Method == http.MethodGet && r.URL.Path == "/api/bots/paul/versions":
					http.Error(w, "history unavailable", http.StatusBadGateway)
				case r.Method == http.MethodPost:
					posts++
					_, _ = io.WriteString(w, `{"id":"created"}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			_, err := New(server.URL, "key").CreateCompetition(t.Context(), competitionConfig("unknown-old", "good-latest"))
			if err == nil || !strings.Contains(err.Error(), "verify Arena competition exclusions") {
				t.Fatalf("expected exclusion verification error, got %v", err)
			}
			if posts != 0 {
				t.Fatalf("competition POST count = %d, want 0", posts)
			}
		})
	}
}

func competitionConfig(players ...string) CompetitionConfig {
	return CompetitionConfig{
		Game: "27td-fl", Players: players, Hands: 100,
		CPUCores: 1, DecisionTimeoutMs: 1000,
	}
}
