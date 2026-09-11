package main

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/sixmaxclone"
)

func constantModel(action sixmaxclone.Action) *sixmaxclone.Model {
	probabilities := [3]float64{}
	probabilities[action] = 1
	leaf := func() sixmaxclone.Tree {
		return sixmaxclone.Tree{Nodes: []sixmaxclone.Node{{Leaf: &sixmaxclone.Leaf{Groups: 30, Probabilities: probabilities}}}}
	}
	return &sixmaxclone.Model{Schema: sixmaxclone.SchemaVersion, MinGroups: 20, MinConfidence: .65,
		Trees: map[string]sixmaxclone.Tree{"facing": leaf(), "checked": leaf()}}
}

func TestConfidenceSelectionRequiresTwentyGroupsAndNonFoldGain(t *testing.T) {
	rows := make([]trainingRow, 20)
	for i := range rows {
		rows[i] = trainingRow{Group: string(rune('a' + i)), Weight: 1, Label: sixmaxclone.Passive,
			Baseline: sixmaxclone.Fold, Available: [3]bool{true, true, true}, Features: sixmaxclone.Features{Facing: true}}
	}
	selected, metrics, err := selectConfidence(constantModel(sixmaxclone.Passive), rows)
	if err != nil || selected != .65 || !metrics[0].Eligible {
		t.Fatalf("selection = %v, eligible %t, err %v", selected, metrics[0].Eligible, err)
	}
}

func TestConfidenceSelectionRejectsFoldOnlyImprovement(t *testing.T) {
	rows := make([]trainingRow, 20)
	for i := range rows {
		rows[i] = trainingRow{Group: string(rune('a' + i)), Weight: 1, Label: sixmaxclone.Fold,
			Baseline: sixmaxclone.Passive, Available: [3]bool{true, true, true}, Features: sixmaxclone.Features{Facing: true}}
	}
	if _, _, err := selectConfidence(constantModel(sixmaxclone.Fold), rows); err == nil {
		t.Fatal("fold-only improvement selected")
	}
}

func TestConfidenceSelectionRejectsNetNonFoldRegression(t *testing.T) {
	rows := make([]trainingRow, 20)
	for i := range rows {
		rows[i] = trainingRow{Group: string(rune('a' + i)), Weight: 1, Available: [3]bool{true, true, true},
			Features: sixmaxclone.Features{Facing: true}}
		if i < 11 {
			rows[i].Label, rows[i].Baseline = sixmaxclone.Fold, sixmaxclone.Passive
		} else {
			rows[i].Label, rows[i].Baseline = sixmaxclone.Aggressive, sixmaxclone.Aggressive
		}
	}
	if _, _, err := selectConfidence(constantModel(sixmaxclone.Fold), rows); err == nil {
		t.Fatal("overall gain with non-fold regression selected")
	}
}

func TestUnavailableLearnedActionUsesBaselineDuringEvaluation(t *testing.T) {
	row := trainingRow{Group: "a", Weight: 1, Label: sixmaxclone.Passive, Baseline: sixmaxclone.Passive,
		Available: [3]bool{false, true, false}, Features: sixmaxclone.Features{}}
	metric := evaluateThreshold(constantModel(sixmaxclone.Aggressive), []trainingRow{row}, .65)
	if metric.ModelAgreement != 1 || metric.OverrideRows != 0 || metric.Coverage != 0 {
		t.Fatalf("unavailable action metrics = %+v", metric)
	}
}
