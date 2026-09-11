package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/nuttakit/2-7-bot/internal/arena"
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/sixmaxclone"
	"github.com/nuttakit/2-7-bot/internal/sixmaxdata"
)

type matchFile struct {
	MatchInfo struct {
		ID       int      `json:"id"`
		Game     string   `json:"game"`
		DealMode string   `json:"dealMode"`
		Players  []string `json:"players"`
	} `json:"matchInfo"`
}

type manifestEntry struct {
	Partition string `json:"partition"`
	MatchID   int    `json:"matchId"`
}

var collectionManifestOverride string

type trainingRow struct {
	Group, Identity string
	Features        sixmaxclone.Features
	Label, Baseline sixmaxclone.Action
	Available       [3]bool
	Weight          float64
	Pot, Call       uint64
}

const supportedRaiseCap = 4

func teacher(name string) bool {
	return name == "swit-27td-ring2" || name == "swit-27td-ring3"
}

func positionEnum(position string) (int, error) {
	switch strings.ToUpper(position) {
	case "BTN":
		return 0, nil
	case "SB":
		return 1, nil
	case "BB":
		return 2, nil
	case "UTG":
		return 3, nil
	case "HJ":
		return 4, nil
	case "CO":
		return 5, nil
	default:
		return 0, fmt.Errorf("unknown position %q", position)
	}
}

func labelAction(label string) (sixmaxclone.Action, error) {
	switch strings.ToLower(label) {
	case "fold":
		return sixmaxclone.Fold, nil
	case "call", "check":
		return sixmaxclone.Passive, nil
	case "bet", "raise":
		return sixmaxclone.Aggressive, nil
	default:
		return 0, fmt.Errorf("unsupported predraw action %q", label)
	}
}

func parseHandStrings(hand []string) ([]cards.Card, error) {
	out := make([]cards.Card, len(hand))
	for i, text := range hand {
		card, err := cards.ParseCard(text)
		if err != nil {
			return nil, err
		}
		out[i] = card
	}
	return out, nil
}

func featuresFromObservation(obs sixmaxdata.Observation, smallBet uint64) (sixmaxclone.Features, uint64, uint64, error) {
	if obs.PotMilli < 0 || obs.ToCallMilli < 0 || obs.StreetAggression < 0 {
		return sixmaxclone.Features{}, 0, 0, fmt.Errorf("event %d has negative public counters", obs.EventID)
	}
	position, err := positionEnum(obs.Position)
	if err != nil {
		return sixmaxclone.Features{}, 0, 0, err
	}
	hand, err := parseHandStrings(obs.Hand)
	if err != nil {
		return sixmaxclone.Features{}, 0, 0, fmt.Errorf("event %d hand: %w", obs.EventID, err)
	}
	calls, ownRaises := 0, 0
	for _, action := range obs.PublicActions {
		if action.Street != "predraw" {
			continue
		}
		if action.AmountMilli < 0 {
			return sixmaxclone.Features{}, 0, 0, fmt.Errorf("event %d has negative public action amount", obs.EventID)
		}
		switch action.Action {
		case "call":
			calls++
		case "raise":
			if action.Player == obs.Player {
				ownRaises++
			}
		}
	}
	pot, call := uint64(obs.PotMilli), uint64(obs.ToCallMilli)
	features, err := sixmaxclone.BuildFeatures(sixmaxclone.Snapshot{Hand: hand, Position: position,
		ActivePlayers: obs.ActivePlayers, Aggressions: obs.StreetAggression, Calls: calls, OwnRaises: ownRaises,
		Pot: pot, Call: call, SmallBet: smallBet})
	return features, pot, call, err
}

func dealFingerprint(record sixmaxdata.HandRecord, raw arena.HandDetail) (string, error) {
	handsByPlayer := make(map[int][]string, 6)
	for _, event := range raw.Events {
		if event.Kind != "initial-cards" || event.Player == nil || event.Cards == nil {
			continue
		}
		if *event.Player < 0 || *event.Player >= 6 || len(*event.Cards) != 10 {
			return "", fmt.Errorf("match %d hand %d has invalid initial-cards event", record.MatchID, record.HandNumber)
		}
		hand := make([]string, 0, 5)
		for i := 0; i < len(*event.Cards); i += 2 {
			hand = append(hand, (*event.Cards)[i:i+2])
		}
		sort.Strings(hand)
		handsByPlayer[*event.Player] = hand
	}
	hands := make(map[string]string, 6)
	for player, seat := range record.Seats {
		hand, ok := handsByPlayer[player]
		if !ok {
			return "", fmt.Errorf("match %d hand %d missing player %d initial hand", record.MatchID, record.HandNumber, player)
		}
		hands[seat.Position] = strings.Join(hand, "")
	}
	positions := [...]string{"BTN", "SB", "BB", "UTG", "HJ", "CO"}
	parts := make([]string, 0, len(positions))
	for _, position := range positions {
		hand, ok := hands[position]
		if !ok {
			return "", fmt.Errorf("match %d hand %d missing %s predraw hand", record.MatchID, record.HandNumber, position)
		}
		parts = append(parts, position+":"+hand)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:]), nil
}

func observationIdentity(record sixmaxdata.HandRecord, obs sixmaxdata.Observation) string {
	return fmt.Sprintf("%d|%d|%d|%d", record.MatchID, record.HandNumber, obs.Player, obs.EventID)
}

func rowsFromRecord(record sixmaxdata.HandRecord, players []string, smallBet uint64, fingerprint string) ([]trainingRow, error) {
	rows := make([]trainingRow, 0, 6)
	for _, obs := range record.Observations {
		if obs.Street != "predraw" || obs.Player < 0 || obs.Player >= len(players) || !teacher(players[obs.Player]) {
			continue
		}
		label, err := labelAction(obs.Label.Action)
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", obs.EventID, err)
		}
		features, pot, call, err := featuresFromObservation(obs, smallBet)
		if err != nil {
			return nil, err
		}
		available := predrawAvailability(call, obs.StreetAggression)
		baseline := baselineAction(features, pot, call, available)
		rows = append(rows, trainingRow{Group: fingerprint, Features: features, Label: label, Baseline: baseline,
			Available: available, Pot: pot, Call: call, Identity: observationIdentity(record, obs)})
	}
	return rows, nil
}

func predrawAvailability(call uint64, voluntaryRaises int) [3]bool {
	return [3]bool{call > 0, true, voluntaryRaises+1 < supportedRaiseCap}
}

func baselineAction(f sixmaxclone.Features, pot, call uint64, available [3]bool) sixmaxclone.Action {
	draw := 5 - f.KeepCount
	strong := f.Category >= int(deuce.Nine) || draw == 1 || (draw == 2 && f.HasDeuce == 1)
	late := f.Position == 2 || f.Position == 3
	playable := strong || (late && draw == 2)
	action := sixmaxclone.Fold
	switch {
	case f.Aggressions >= 2:
		if f.Category >= int(deuce.Eight) || draw == 1 || (f.Aggressions == 2 && f.OwnRaises == 1 && f.KeepCount == 3 && f.HasDeuce == 1 && f.HighestKeep <= int(cards.Seven) && affordableOffline(pot, call, 5)) {
			action = sixmaxclone.Passive
		}
	case f.Aggressions == 1:
		if f.Category >= int(deuce.Eight) {
			action = sixmaxclone.Aggressive
		} else if strong {
			action = sixmaxclone.Passive
		}
	case f.Calls > 0:
		if strong {
			action = sixmaxclone.Aggressive
		} else if playable {
			action = sixmaxclone.Passive
		}
	case playable:
		action = sixmaxclone.Aggressive
	}
	if available[action] {
		return action
	}
	return sixmaxclone.Passive
}

func affordableOffline(pot, call, denominator uint64) bool {
	return denominator > 0 && (call == 0 || call <= (pot+call)/denominator)
}

func readRawHand(path string) (arena.HandDetail, uint64, error) {
	var hand arena.HandDetail
	if err := readJSON(path, &hand); err != nil {
		return hand, 0, err
	}
	var bigBlind uint64
	for _, event := range hand.Events {
		if event.Kind != "forced" || event.AmountMilli == nil {
			continue
		}
		if *event.AmountMilli < 0 {
			return hand, 0, fmt.Errorf("%s has negative forced payment", path)
		}
		action := ""
		if event.Action != nil {
			action = *event.Action
		}
		if action == "ante" && *event.AmountMilli != 0 {
			return hand, 0, fmt.Errorf("%s has unsupported nonzero ante", path)
		}
		if action == "big-blind" {
			if bigBlind != 0 || *event.AmountMilli == 0 {
				return hand, 0, fmt.Errorf("%s has invalid big blind", path)
			}
			bigBlind = uint64(*event.AmountMilli)
		}
	}
	if bigBlind == 0 {
		return hand, 0, fmt.Errorf("%s has no positive big blind", path)
	}
	return hand, bigBlind, nil
}

func loadMatch(dir string) ([]trainingRow, error) {
	requestedID, requestedErr := strconv.Atoi(strings.TrimPrefix(filepath.Base(filepath.Clean(dir)), "match-"))
	if requestedErr != nil {
		return nil, fmt.Errorf("invalid requested match path %s", dir)
	}
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return nil, err
	}
	if forbiddenTestPath(canonical) {
		return nil, fmt.Errorf("locked test path %s cannot be used by clone fitter", dir)
	}
	var meta matchFile
	if err := readJSON(filepath.Join(canonical, "match.json"), &meta); err != nil {
		return nil, err
	}
	if meta.MatchInfo.ID == 103 {
		return nil, fmt.Errorf("locked match 103 cannot be used by clone fitter")
	}
	locked, err := manifestLocksMatch(meta.MatchInfo.ID)
	if err != nil {
		return nil, err
	}
	if locked {
		return nil, fmt.Errorf("manifest marks match %d as locked test data", meta.MatchInfo.ID)
	}
	baseID, parseErr := strconv.Atoi(strings.TrimPrefix(filepath.Base(canonical), "match-"))
	if parseErr != nil || baseID != meta.MatchInfo.ID || requestedID != meta.MatchInfo.ID {
		return nil, fmt.Errorf("path match identity does not match metadata id %d", meta.MatchInfo.ID)
	}
	if meta.MatchInfo.Game != "27td-fl" || meta.MatchInfo.DealMode != "duplicate" || len(meta.MatchInfo.Players) != 6 {
		return nil, fmt.Errorf("match %d must be six-player duplicate mode", meta.MatchInfo.ID)
	}
	var records []sixmaxdata.HandRecord
	if err := readJSON(filepath.Join(canonical, "derived.json"), &records); err != nil {
		return nil, err
	}
	var rows []trainingRow
	fingerprints := map[string]string{}
	for _, record := range records {
		if record.MatchID != meta.MatchInfo.ID {
			return nil, fmt.Errorf("match %d derived record claims match %d", meta.MatchInfo.ID, record.MatchID)
		}
		raw, smallBet, err := readRawHand(filepath.Join(canonical, "hands", strconv.Itoa(record.HandNumber)+".json"))
		if err != nil {
			return nil, err
		}
		if raw.Hand.Number != record.HandNumber {
			return nil, fmt.Errorf("raw hand number %d does not match derived hand %d", raw.Hand.Number, record.HandNumber)
		}
		fingerprint, err := dealFingerprint(record, raw)
		if err != nil {
			return nil, err
		}
		if previous, ok := fingerprints[record.SplitKey]; ok && previous != fingerprint {
			return nil, fmt.Errorf("duplicate group %s contains different deal fingerprints", record.SplitKey)
		}
		fingerprints[record.SplitKey] = fingerprint
		got, err := rowsFromRecord(record, meta.MatchInfo.Players, smallBet, fingerprint)
		if err != nil {
			return nil, err
		}
		rows = append(rows, got...)
	}
	return rows, nil
}

func manifestLocksMatch(matchID int) (bool, error) {
	candidates := []string{collectionManifestOverride}
	if collectionManifestOverride == "" {
		candidates = []string{
			filepath.Join("bin", "sixmax", "clone-data", "manifest.json"),
			filepath.Join("..", "..", "bin", "sixmax", "clone-data", "manifest.json"),
		}
	}
	for _, path := range candidates {
		if path == "" {
			continue
		}
		var entries []manifestEntry
		if err := readJSON(path, &entries); err != nil {
			if os.IsNotExist(err) && collectionManifestOverride == "" {
				continue
			}
			return false, err
		}
		for _, entry := range entries {
			if entry.MatchID == matchID && strings.EqualFold(entry.Partition, "test") {
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil
}

func deduplicateAndWeight(rows []trainingRow) ([]trainingRow, error) {
	seen := make(map[string]trainingRow, len(rows))
	deduplicated := make([]trainingRow, 0, len(rows))
	counts := map[string]int{}
	for _, row := range rows {
		if previous, ok := seen[row.Identity]; ok {
			if !reflect.DeepEqual(previous, row) {
				return nil, fmt.Errorf("observation identity %s has conflicting rows", row.Identity)
			}
			continue
		}
		seen[row.Identity] = row
		deduplicated = append(deduplicated, row)
		counts[row.Group]++
	}
	for i := range deduplicated {
		deduplicated[i].Weight = 1 / float64(counts[deduplicated[i].Group])
	}
	return deduplicated, nil
}

func splitByFingerprint(rows []trainingRow) (train, validation []trainingRow) {
	for _, row := range rows {
		hash, _ := hex.DecodeString(row.Group)
		if len(hash) > 0 && hash[0]%5 == 0 {
			validation = append(validation, row)
		} else {
			train = append(train, row)
		}
	}
	return
}

func assertDisjoint(train, validation []trainingRow) error {
	groups := map[string]bool{}
	for _, row := range train {
		groups[row.Group] = true
	}
	for _, row := range validation {
		if groups[row.Group] {
			return fmt.Errorf("deal fingerprint %s appears in training and validation", row.Group)
		}
	}
	return nil
}

func readJSON(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
