package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/arena"
	"github.com/nuttakit/2-7-bot/internal/sixmaxclone"
	"github.com/nuttakit/2-7-bot/internal/sixmaxdata"
)

func TestFeaturesUseSharedPositionAndPublicPrefixMapping(t *testing.T) {
	tests := []struct {
		name               string
		position           string
		call               int64
		aggressions, calls int
		ownRaises          bool
		wantPosition       int
		wantFacing         bool
	}{
		{"UTG", "UTG", 500, 0, 0, false, 0, true},
		{"SB", "SB", 250, 0, 0, false, 4, true},
		{"limped BB", "BB", 0, 0, 1, false, 5, false},
		{"BB versus open", "BB", 500, 1, 0, false, 5, true},
		{"own opener faces three-bet", "CO", 500, 2, 0, true, 2, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			obs := sixmaxdata.Observation{EventID: 10, Player: 4, Position: tc.position, Street: "predraw",
				Hand: []string{"2c", "3d", "7h", "Js", "Kc"}, PotMilli: 1500, ToCallMilli: tc.call,
				ActivePlayers: 6, StreetAggression: tc.aggressions}
			for i := 0; i < tc.calls; i++ {
				obs.PublicActions = append(obs.PublicActions, sixmaxdata.PublicAction{Player: i, Street: "predraw", Action: "call"})
			}
			if tc.ownRaises {
				obs.PublicActions = append(obs.PublicActions, sixmaxdata.PublicAction{Player: obs.Player, Street: "predraw", Action: "raise", AmountMilli: 1000})
			}
			features, _, _, err := featuresFromObservation(obs, 500)
			if err != nil {
				t.Fatal(err)
			}
			if features.Position != tc.wantPosition || features.Facing != tc.wantFacing || features.Aggressions != tc.aggressions || features.Calls != tc.calls || features.OwnRaises != btoi(tc.ownRaises) {
				t.Fatalf("features = %+v", features)
			}
		})
	}
}

func TestFeatureExtractionIgnoresLabelAndUnrelatedRecordFields(t *testing.T) {
	obs := sixmaxdata.Observation{EventID: 10, Player: 0, Position: "UTG", Street: "predraw",
		Hand: []string{"2c", "3d", "7h", "Js", "Kc"}, PotMilli: 750, ToCallMilli: 500,
		ActivePlayers: 6, Label: sixmaxdata.Label{Action: "raise", AmountMilli: 1000}}
	want, _, _, err := featuresFromObservation(obs, 500)
	if err != nil {
		t.Fatal(err)
	}
	mutated := obs
	mutated.EventID = 999
	mutated.Label = sixmaxdata.Label{Action: "fold", AmountMilli: 999999, DrawCount: 5, Discard: []string{"Ac"}}
	got, _, _, err := featuresFromObservation(mutated, 500)
	if err != nil {
		t.Fatal(err)
	}
	record := sixmaxdata.HandRecord{Observations: []sixmaxdata.Observation{mutated, {Street: "draw3"}},
		FinalDrawRanks: []sixmaxdata.FinalDrawRank{{Player: 0, Value: 123}}}
	players := []string{"renamed", "future", "outcome", "does", "not", "matter"}
	_, _ = record, players
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mutated features = %+v, want %+v", got, want)
	}
}

func TestFeatureExtractionIsChipScaleInvariant(t *testing.T) {
	obs := sixmaxdata.Observation{EventID: 10, Player: 0, Position: "UTG", Street: "predraw",
		Hand: []string{"2c", "3d", "7h", "Js", "Kc"}, PotMilli: 1750, ToCallMilli: 500, ActivePlayers: 6}
	want, _, _, err := featuresFromObservation(obs, 500)
	if err != nil {
		t.Fatal(err)
	}
	obs.PotMilli *= 10
	obs.ToCallMilli *= 10
	got, _, _, err := featuresFromObservation(obs, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scaled features = %+v, want %+v", got, want)
	}
}

func TestPredrawAvailabilityCountsBigBlindTowardRaiseCap(t *testing.T) {
	if available := predrawAvailability(500, 2); !available[sixmaxclone.Aggressive] {
		t.Fatal("two voluntary raises should leave the final full wager available")
	}
	if available := predrawAvailability(500, 3); available[sixmaxclone.Aggressive] || !available[sixmaxclone.Passive] || !available[sixmaxclone.Fold] {
		t.Fatalf("capped facing availability = %v", available)
	}
	if available := predrawAvailability(0, 3); available[sixmaxclone.Aggressive] || available[sixmaxclone.Fold] || !available[sixmaxclone.Passive] {
		t.Fatalf("capped checked availability = %v", available)
	}
}

func TestDeduplicateRowsAndNormalizeEachDealToOne(t *testing.T) {
	rows := []trainingRow{
		{Group: "a", Identity: "a1"}, {Group: "a", Identity: "a1"}, {Group: "a", Identity: "a2"},
		{Group: "b", Identity: "b1"},
	}
	got, err := deduplicateAndWeight(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("rows = %d, want 3", len(got))
	}
	totals := map[string]float64{}
	for _, row := range got {
		totals[row.Group] += row.Weight
	}
	if totals["a"] != 1 || totals["b"] != 1 {
		t.Fatalf("group weights = %v", totals)
	}
}

func TestCrossPartitionDuplicateFingerprintRejected(t *testing.T) {
	if err := assertDisjoint([]trainingRow{{Group: "same"}}, []trainingRow{{Group: "same"}}); err == nil {
		t.Fatal("cross-partition duplicate accepted")
	}
}

func TestDealFingerprintUsesRawCardsWhenBigBlindNeverActsAndSurvivesRotation(t *testing.T) {
	positions := [...]string{"BTN", "SB", "BB", "UTG", "HJ", "CO"}
	hands := [...]string{"2c3d4h7sKc", "2d3h5s8cQd", "2h4d6s9cKh", "3c5d7hTsAc", "4c6d8hJsAd", "5c7d9hQsAh"}
	makeDeal := func(rotation int) (sixmaxdata.HandRecord, arena.HandDetail) {
		record := sixmaxdata.HandRecord{MatchID: 41, HandNumber: rotation + 1}
		raw := arena.HandDetail{}
		for position := range positions {
			player := (position + rotation) % 6
			record.Seats[player] = sixmaxdata.Seat{Player: player, Position: positions[position]}
			p, cards := player, hands[position]
			raw.Events = append(raw.Events, arena.HandEvent{Kind: "initial-cards", Player: &p, Cards: &cards})
			if positions[position] != "BB" {
				record.Observations = append(record.Observations, sixmaxdata.Observation{Player: player, Position: positions[position], Street: "predraw"})
			}
		}
		return record, raw
	}
	firstRecord, firstRaw := makeDeal(0)
	secondRecord, secondRaw := makeDeal(3)
	first, err := dealFingerprint(firstRecord, firstRaw)
	if err != nil {
		t.Fatal(err)
	}
	second, err := dealFingerprint(secondRecord, secondRaw)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("rotated fingerprints differ: %s != %s", first, second)
	}
}

func TestRawHandUsesBigBlindAndRejectsNonzeroAnte(t *testing.T) {
	event := func(action string, amount int64) arena.HandEvent {
		player := 0
		return arena.HandEvent{Kind: "forced", Player: &player, Action: &action, AmountMilli: &amount}
	}
	path := filepath.Join(t.TempDir(), "hand.json")
	hand := arena.HandDetail{Events: []arena.HandEvent{event("small-blind", 250), event("big-blind", 500)}}
	if err := writeJSON(path, hand); err != nil {
		t.Fatal(err)
	}
	if _, smallBet, err := readRawHand(path); err != nil || smallBet != 500 {
		t.Fatalf("small bet = %d, err %v", smallBet, err)
	}
	hand.Events = append(hand.Events, event("ante", 50))
	if err := writeJSON(path, hand); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readRawHand(path); err == nil {
		t.Fatal("nonzero ante accepted")
	}
}

func TestDistinctEqualFeatureEventsArePreservedAndConflictsRejected(t *testing.T) {
	first := trainingRow{Group: "a", Identity: "41|1|0|10", Label: 1}
	second := first
	second.Identity = "41|2|0|10"
	got, err := deduplicateAndWeight([]trainingRow{first, second})
	if err != nil || len(got) != 2 {
		t.Fatalf("distinct events = %d, err %v", len(got), err)
	}
	conflict := first
	conflict.Label = 2
	if _, err := deduplicateAndWeight([]trainingRow{first, conflict}); err == nil {
		t.Fatal("conflicting stable observation identity accepted")
	}
}

func TestCommonLoaderRejectsCanonicalTestPathAndManifestTestID(t *testing.T) {
	root := t.TempDir()
	locked := filepath.Join(root, "test", "match-41")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "dev", "match-41")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(locked, link); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMatch(link); err == nil || !strings.Contains(err.Error(), "locked test path") {
		t.Fatalf("symlinked test path error = %v", err)
	}

	dev := filepath.Join(root, "dev", "match-77")
	writeMatchMeta(t, dev, 77, "27td-fl")
	manifest := filepath.Join(root, "manifest.json")
	if err := writeJSON(manifest, []manifestEntry{{Partition: "test", MatchID: 77}}); err != nil {
		t.Fatal(err)
	}
	old := collectionManifestOverride
	collectionManifestOverride = manifest
	t.Cleanup(func() { collectionManifestOverride = old })
	if _, err := loadMatch(dev); err == nil || !strings.Contains(err.Error(), "manifest marks") {
		t.Fatalf("manifest test id error = %v", err)
	}
}

func TestCommonLoaderValidatesGamePathIdentityAndRawHandNumber(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "manifest.json")
	if err := writeJSON(manifest, []manifestEntry{}); err != nil {
		t.Fatal(err)
	}
	old := collectionManifestOverride
	collectionManifestOverride = manifest
	t.Cleanup(func() { collectionManifestOverride = old })

	wrongGame := filepath.Join(root, "match-41")
	writeMatchMeta(t, wrongGame, 41, "holdem")
	if _, err := loadMatch(wrongGame); err == nil || !strings.Contains(err.Error(), "six-player duplicate") {
		t.Fatalf("wrong game error = %v", err)
	}
	crossCopied := filepath.Join(root, "match-42")
	writeMatchMeta(t, crossCopied, 41, "27td-fl")
	if _, err := loadMatch(crossCopied); err == nil || !strings.Contains(err.Error(), "path match identity") {
		t.Fatalf("cross-copy error = %v", err)
	}

	dir := filepath.Join(root, "match-43")
	writeMatchMeta(t, dir, 43, "27td-fl")
	record := sixmaxdata.HandRecord{MatchID: 43, HandNumber: 1, SplitKey: "43:1"}
	positions := [...]string{"BTN", "SB", "BB", "UTG", "HJ", "CO"}
	hands := [...]string{"2c3d4h7sKc", "2d3h5s8cQd", "2h4d6s9cKh", "3c5d7hTsAc", "4c6d8hJsAd", "5c7d9hQsAh"}
	raw := arena.HandDetail{}
	raw.Hand.Number = 2
	for player := range positions {
		record.Seats[player] = sixmaxdata.Seat{Player: player, Position: positions[player]}
		p, cards := player, hands[player]
		raw.Events = append(raw.Events, arena.HandEvent{Kind: "initial-cards", Player: &p, Cards: &cards})
	}
	action, amount, player := "big-blind", int64(500), 2
	raw.Events = append(raw.Events, arena.HandEvent{Kind: "forced", Player: &player, Action: &action, AmountMilli: &amount})
	if err := writeJSON(filepath.Join(dir, "derived.json"), []sixmaxdata.HandRecord{record}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, "hands", "1.json"), raw); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMatch(dir); err == nil || !strings.Contains(err.Error(), "raw hand number") {
		t.Fatalf("raw number error = %v", err)
	}
}

func TestCommonLoaderRejectsRequestedSymlinkIdentityMismatch(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "manifest.json")
	if err := writeJSON(manifest, []manifestEntry{}); err != nil {
		t.Fatal(err)
	}
	old := collectionManifestOverride
	collectionManifestOverride = manifest
	t.Cleanup(func() { collectionManifestOverride = old })

	target := filepath.Join(root, "match-77")
	writeMatchMeta(t, target, 77, "27td-fl")
	link := filepath.Join(root, "match-41")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMatch(link); err == nil || !strings.Contains(err.Error(), "path match identity") {
		t.Fatalf("requested symlink identity error = %v", err)
	}
}

func TestEndToEndLoaderFixtureBuildsRowsAndProtectsGroupedSplit(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "manifest.json")
	if err := writeJSON(manifest, []manifestEntry{}); err != nil {
		t.Fatal(err)
	}
	old := collectionManifestOverride
	collectionManifestOverride = manifest
	t.Cleanup(func() { collectionManifestOverride = old })

	dir := filepath.Join(root, "match-41")
	writeMatchMeta(t, dir, 41, "27td-fl")
	positions := [...]string{"BTN", "SB", "BB", "UTG", "HJ", "CO"}
	packed := [...]string{"2c3d4h7sKc", "2d3h5s8cQd", "2h4d6s9cKh", "3c5d7hTsAc", "4c6d8hJsAd", "5c7d9hQsAh"}
	record := sixmaxdata.HandRecord{MatchID: 41, HandNumber: 1, SplitKey: "41:1"}
	raw := arena.HandDetail{}
	raw.Hand.Number = 1
	for player, position := range positions {
		record.Seats[player] = sixmaxdata.Seat{Player: player, Position: position}
		p, cards := player, packed[player]
		raw.Events = append(raw.Events, arena.HandEvent{Kind: "initial-cards", Player: &p, Cards: &cards})
		hand := make([]string, 0, 5)
		for i := 0; i < len(cards); i += 2 {
			hand = append(hand, cards[i:i+2])
		}
		record.Observations = append(record.Observations, sixmaxdata.Observation{EventID: int64(10 + player),
			Player: player, Position: position, Street: "predraw", Hand: hand, PotMilli: 750,
			ToCallMilli: 500, ActivePlayers: 6, Label: sixmaxdata.Label{Action: "raise", AmountMilli: 1000}})
	}
	action, amount, player := "big-blind", int64(500), 2
	raw.Events = append(raw.Events, arena.HandEvent{Kind: "forced", Player: &player, Action: &action, AmountMilli: &amount})
	if err := writeJSON(filepath.Join(dir, "derived.json"), []sixmaxdata.HandRecord{record}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, "hands", "1.json"), raw); err != nil {
		t.Fatal(err)
	}

	rows, err := loadPaths([]string{dir, dir})
	if err != nil {
		t.Fatal(err)
	}
	rows, err = deduplicateAndWeight(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Label != sixmaxclone.Aggressive || rows[0].Weight != 1 {
		t.Fatalf("loaded rows = %+v", rows)
	}
	train, validation := splitByFingerprint(rows)
	if err := assertDisjoint(train, validation); err != nil {
		t.Fatal(err)
	}
	if len(train)+len(validation) != 1 {
		t.Fatalf("split rows = %d/%d", len(train), len(validation))
	}
}

func TestWholeTrainingMatchesDoNotEnterDevelopmentSplit(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "manifest.json")
	if err := writeJSON(manifest, []manifestEntry{}); err != nil {
		t.Fatal(err)
	}
	old := collectionManifestOverride
	collectionManifestOverride = manifest
	t.Cleanup(func() { collectionManifestOverride = old })

	dir := filepath.Join(root, "match-41")
	writeMatchMeta(t, dir, 41, "27td-fl")
	positions := [...]string{"BTN", "SB", "BB", "UTG", "HJ", "CO"}
	packed := [...]string{"2c3d4h7sKc", "2d3h5s8cQd", "2h4d6s9cKh", "3c5d7hTsAc", "4c6d8hJsAd", "5c7d9hQsAh"}
	record := sixmaxdata.HandRecord{MatchID: 41, HandNumber: 1, SplitKey: "41:1"}
	raw := arena.HandDetail{}
	raw.Hand.Number = 1
	for player, position := range positions {
		record.Seats[player] = sixmaxdata.Seat{Player: player, Position: position}
		p, cards := player, packed[player]
		raw.Events = append(raw.Events, arena.HandEvent{Kind: "initial-cards", Player: &p, Cards: &cards})
	}
	record.Observations = []sixmaxdata.Observation{{EventID: 10, Player: 0, Position: "BTN", Street: "predraw",
		Hand: []string{"2c", "3d", "4h", "7s", "Kc"}, PotMilli: 750, ToCallMilli: 500, ActivePlayers: 6,
		Label: sixmaxdata.Label{Action: "raise", AmountMilli: 1000}}}
	action, amount, player := "big-blind", int64(500), 2
	raw.Events = append(raw.Events, arena.HandEvent{Kind: "forced", Player: &player, Action: &action, AmountMilli: &amount})
	if err := writeJSON(filepath.Join(dir, "derived.json"), []sixmaxdata.HandRecord{record}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, "hands", "1.json"), raw); err != nil {
		t.Fatal(err)
	}

	rows, sources, err := loadWholeMatches(root, "41")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || len(sources) != 1 || sources[0] != "training:match-41" {
		t.Fatalf("whole training source rows=%d sources=%v", len(rows), sources)
	}
}

func writeMatchMeta(t *testing.T, dir string, id int, game string) {
	t.Helper()
	meta := matchFile{}
	meta.MatchInfo.ID, meta.MatchInfo.Game, meta.MatchInfo.DealMode = id, game, "duplicate"
	meta.MatchInfo.Players = []string{"swit-27td-ring2", "x", "x", "x", "x", "x"}
	if err := writeJSON(filepath.Join(dir, "match.json"), meta); err != nil {
		t.Fatal(err)
	}
}

func btoi(value bool) int {
	if value {
		return 1
	}
	return 0
}
