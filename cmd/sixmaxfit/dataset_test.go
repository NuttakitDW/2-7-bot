package main

import (
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/sixmaxdata"
	"testing"
)

func TestRowsUseFinalRanksAndNeverCurrentHandLabelOrFuture(t *testing.T) {
	base := sixmaxdata.Observation{EventID: 10, Player: 0, Street: "draw3", ActivePlayers: 2, PublicActions: []sixmaxdata.PublicAction{{Player: 1, Street: "draw3", Action: "check"}}, Label: sixmaxdata.Label{Action: "bet"}}
	for p := range base.DrawHistory {
		base.DrawHistory[p] = [3]int{-1, -1, -1}
	}
	base.DrawHistory[1] = [3]int{2, 1, 0}
	a := base
	a.Hand = []string{"2c", "3d", "4h", "5s", "7c"}
	a.Label.AmountMilli = 999
	b := base
	b.Hand = []string{"Ac"}
	b.Label.Action = "fold"
	rank := deuce.Value(12345)
	record := sixmaxdata.HandRecord{MatchID: 41, HandNumber: 1, SplitKey: "41:1", Observations: []sixmaxdata.Observation{a, b}, FinalDrawRanks: []sixmaxdata.FinalDrawRank{{Player: 1, Value: rank}}}
	rows := rowsFromHand(record, []string{"actor", "swit-27td-ring3"}, "duplicate")
	if len(rows) != 1 {
		t.Fatalf("deduplicated rows=%d", len(rows))
	}
	if rows[0].Value != rank || rows[0].Context.LastAction != "check" {
		t.Fatalf("row leaked observation: %+v", rows[0])
	}
}
