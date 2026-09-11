package main

import (
	"fmt"
	"strings"

	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/sixmaxdata"
	"github.com/nuttakit/2-7-bot/internal/sixmaxrange"
)

func eligibleStrong(name string) bool {
	for _, eligible := range eligibleNames() {
		if name == eligible {
			return true
		}
	}
	return false
}

func rowsFromHand(record sixmaxdata.HandRecord, players []string, dealMode string) []sixmaxrange.Row {
	ranks := map[int]deuce.Value{}
	for _, rank := range record.FinalDrawRanks {
		ranks[rank.Player] = rank.Value
	}
	weight := 1.0
	if dealMode == "duplicate" {
		weight = 1.0 / 6.0
	}
	seen := map[string]bool{}
	rows := []sixmaxrange.Row{}
	for _, obs := range record.Observations {
		if obs.Street != "draw3" || obs.Label.Action == "draw" {
			continue
		}
		folded := map[int]bool{}
		actions := make([]sixmaxrange.PublicAction, 0, len(obs.PublicActions))
		for _, action := range obs.PublicActions {
			actions = append(actions, sixmaxrange.PublicAction{Seat: action.Player, Street: streetIndex(action.Street), Action: action.Action})
			if action.Action == "fold" {
				folded[action.Player] = true
			}
		}
		for target, value := range ranks {
			if target == obs.Player || folded[target] || target < 0 || target >= len(players) || !eligibleStrong(players[target]) {
				continue
			}
			ctx, known := sixmaxrange.BuildContext(target, obs.DrawHistory, actions, obs.ActivePlayers)
			if !known {
				continue
			}
			id := fmt.Sprintf("%d|%d|%d|%v", record.MatchID, record.HandNumber, target, ctx)
			if seen[id] {
				continue
			}
			seen[id] = true
			rows = append(rows, sixmaxrange.Row{MatchID: record.MatchID, Hand: record.HandNumber, Group: record.SplitKey, Target: target, Context: ctx, Value: value, Weight: weight})
		}
	}
	return rows
}

func streetIndex(street string) int {
	switch strings.ToLower(street) {
	case "predraw":
		return 0
	case "draw1":
		return 1
	case "draw2":
		return 2
	case "draw3":
		return 3
	}
	return -1
}
