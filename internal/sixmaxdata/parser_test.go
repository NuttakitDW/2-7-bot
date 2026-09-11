package sixmaxdata

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/arena"
)

func TestParseHandMapsDealOrderAndUsesOnlyPublicPrefix(t *testing.T) {
	text := func(s string) *string { return &s }
	player := func(p int) *int { return &p }
	amount := func(n int64) *int64 { return &n }
	h := arena.HandDetail{
		Hand: arena.HandSummary{Number: 7, Roles: []string{"button", "other", "other", "bigblind", "other", "smallblind"}},
		Events: []arena.HandEvent{
			{ID: 1, Kind: "forced", Player: player(5), Action: text("small-blind"), AmountMilli: amount(250)},
			{ID: 2, Kind: "forced", Player: player(3), Action: text("big-blind"), AmountMilli: amount(500)},
			{ID: 3, Kind: "initial-cards", Player: player(5), Street: text("predraw"), Cards: text("Kh8hQh6dTs")},
			{ID: 4, Kind: "initial-cards", Player: player(3), Street: text("predraw"), Cards: text("Ah3h4cQd4h")},
			{ID: 5, Kind: "initial-cards", Player: player(2), Street: text("predraw"), Cards: text("5c3sTcKs2d")},
			{ID: 6, Kind: "initial-cards", Player: player(4), Street: text("predraw"), Cards: text("7h5dJd5h7d")},
			{ID: 7, Kind: "initial-cards", Player: player(1), Street: text("predraw"), Cards: text("As2h6s3dAc")},
			{ID: 8, Kind: "initial-cards", Player: player(0), Street: text("predraw"), Cards: text("5s2cAdQs9d")},
			{ID: 9, Kind: "action", Player: player(2), Street: text("predraw"), Action: text("raise"), AmountMilli: amount(1000)},
			{ID: 10, Kind: "action", Player: player(4), Street: text("predraw"), Action: text("fold"), AmountMilli: amount(0)},
		},
	}

	record, err := ParseHand(41, "duplicate", h)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := record.Seats[2].Position, "UTG"; got != want {
		t.Fatalf("player 2 position = %q, want %q", got, want)
	}
	if got, want := record.Seats[0].Position, "BTN"; got != want {
		t.Fatalf("player 0 position = %q, want %q", got, want)
	}
	if got, want := record.SplitKey, "41:2"; got != want {
		t.Fatalf("split key = %q, want %q", got, want)
	}
	if len(record.Observations) != 2 {
		t.Fatalf("observations = %d, want 2", len(record.Observations))
	}
	first := record.Observations[0]
	if first.Player != 2 || first.Position != "UTG" || first.ToCallMilli != 500 || first.PotMilli != 750 {
		t.Fatalf("first observation = %+v", first)
	}
	if len(first.PublicActions) != 0 {
		t.Fatalf("future/current action leaked into prefix: %+v", first.PublicActions)
	}
	if got := first.Hand; len(got) != 5 || got[0] != "5c" {
		t.Fatalf("private hand = %v", got)
	}
	if second := record.Observations[1]; len(second.PublicActions) != 1 || second.PublicActions[0].Action != "raise" {
		t.Fatalf("second prefix = %+v", second.PublicActions)
	}
}

func TestParseHandTracksReplacementAndFinalDrawRanks(t *testing.T) {
	text := func(s string) *string { return &s }
	player := func(p int) *int { return &p }
	h := arena.HandDetail{Hand: arena.HandSummary{Number: 13, Roles: make([]string, 6)}, Events: []arena.HandEvent{}}
	order := []int{5, 3, 2, 4, 1, 0}
	cards := []string{"2c3d4h7sKc", "2d3h5s8cQd", "2h4d6s9cKh", "3c5d7hTsAc", "4c6d8hJsAd", "5c7d9hQsAh"}
	for i, p := range order {
		h.Events = append(h.Events, arena.HandEvent{Kind: "initial-cards", Player: player(p), Street: text("predraw"), Cards: text(cards[i])})
	}
	h.Events = append(h.Events,
		arena.HandEvent{Kind: "replacement", Player: player(2), Street: text("draw1"), Cards: text("8s"), Detail: text("Kh")},
		arena.HandEvent{Kind: "action", Player: player(2), Street: text("draw1"), Action: text("check")},
		arena.HandEvent{Kind: "replacement", Player: player(2), Street: text("draw2"), Cards: text("7c"), Detail: text("9c")},
		arena.HandEvent{Kind: "replacement", Player: player(2), Street: text("draw3"), Cards: text("5h"), Detail: text("8s")},
		arena.HandEvent{Kind: "action", Player: player(2), Street: text("draw3"), Action: text("fold")},
	)
	record, err := ParseHand(9, "seeded", h)
	if err != nil {
		t.Fatal(err)
	}
	if got := record.Observations[1].Hand; got[4] != "8s" {
		t.Fatalf("replacement not reflected in later hand: %v", got)
	}
	if got := record.Observations[1].DrawHistory[2]; got != [3]int{1, -1, -1} {
		t.Fatalf("draw history = %v", got)
	}
	if len(record.FinalDrawRanks) != 1 || record.FinalDrawRanks[0].Player != 2 || record.FinalDrawRanks[0].Value == 0 {
		t.Fatalf("final ranks = %+v", record.FinalDrawRanks)
	}
	if got := record.Observations[len(record.Observations)-1].ActivePlayers; got != 6 {
		t.Fatalf("active players before fold = %d", got)
	}
}

func TestParseHandRejectsMalformedReplacement(t *testing.T) {
	text := func(s string) *string { return &s }
	p := 0
	h := arena.HandDetail{Hand: arena.HandSummary{Number: 1, Roles: make([]string, 6)}, Events: []arena.HandEvent{
		{Kind: "initial-cards", Player: &p, Cards: text("2c3d4h7sKc")},
		{Kind: "replacement", Player: &p, Street: text("draw1"), Cards: text("8s9s"), Detail: text("Kc")},
	}}
	if _, err := ParseHand(1, "seeded", h); err == nil {
		t.Fatal("expected unbalanced replacement error")
	}
}

func TestParseHandPreservesZeroCardFinalDrawAndFoldedDeadMoney(t *testing.T) {
	text := func(s string) *string { return &s }
	player := func(p int) *int { return &p }
	amount := func(n int64) *int64 { return &n }
	h := arena.HandDetail{Hand: arena.HandSummary{Number: 1, Roles: make([]string, 6)}}
	order := []int{5, 3, 2, 4, 1, 0}
	deals := []string{"2c3d4h7sKc", "2d3h5s8cQd", "2h4d6s9cKh", "3c5d7hTsAc", "4c6d8hJsAd", "5c7d9hQsAh"}
	for i, p := range order {
		h.Events = append(h.Events, arena.HandEvent{Kind: "initial-cards", Player: player(p), Cards: text(deals[i])})
	}
	h.Events = append(h.Events,
		arena.HandEvent{Kind: "action", Player: player(4), Street: text("predraw"), Action: text("call"), AmountMilli: amount(500)},
		arena.HandEvent{Kind: "action", Player: player(4), Street: text("predraw"), Action: text("fold")},
		arena.HandEvent{Kind: "replacement", Player: player(2), Street: text("draw3"), Cards: text(""), Detail: text("")},
		arena.HandEvent{Kind: "action", Player: player(2), Street: text("draw3"), Action: text("check")},
	)
	record, err := ParseHand(4, "seeded", h)
	if err != nil {
		t.Fatal(err)
	}
	last := record.Observations[len(record.Observations)-1]
	if last.PotMilli != 500 || last.DeadPotMilli != 500 || last.ActivePlayers != 5 {
		t.Fatalf("dead money/active state = pot %d dead %d active %d", last.PotMilli, last.DeadPotMilli, last.ActivePlayers)
	}
	if got := last.DrawHistory[2][2]; got != 0 {
		t.Fatalf("stand-pat draw = %d", got)
	}
}

func TestFoldedPlayerDoesNotSetAmountToCall(t *testing.T) {
	text := func(s string) *string { return &s }
	player := func(p int) *int { return &p }
	amount := func(n int64) *int64 { return &n }
	h := arena.HandDetail{Hand: arena.HandSummary{Number: 1, Roles: make([]string, 6)}}
	order := []int{5, 3, 2, 4, 1, 0}
	deals := []string{"2c3d4h7sKc", "2d3h5s8cQd", "2h4d6s9cKh", "3c5d7hTsAc", "4c6d8hJsAd", "5c7d9hQsAh"}
	for i, p := range order {
		h.Events = append(h.Events, arena.HandEvent{Kind: "initial-cards", Player: player(p), Cards: text(deals[i])})
	}
	h.Events = append(h.Events,
		arena.HandEvent{Kind: "action", Player: player(2), Street: text("predraw"), Action: text("raise"), AmountMilli: amount(1000)},
		arena.HandEvent{Kind: "action", Player: player(2), Street: text("predraw"), Action: text("fold")},
		arena.HandEvent{Kind: "action", Player: player(4), Street: text("predraw"), Action: text("check")},
	)
	record, err := ParseHand(1, "seeded", h)
	if err != nil {
		t.Fatal(err)
	}
	last := record.Observations[len(record.Observations)-1]
	if last.ToCallMilli != 0 || last.DeadPotMilli != 1000 {
		t.Fatalf("after raiser folds: %+v", last)
	}
}
