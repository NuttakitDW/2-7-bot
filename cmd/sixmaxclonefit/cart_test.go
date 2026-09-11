package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/sixmaxclone"
)

func syntheticRows(groups int, facing bool) []trainingRow {
	rows := make([]trainingRow, groups)
	for i := range rows {
		label := sixmaxclone.Passive
		if i >= groups/2 {
			label = sixmaxclone.Aggressive
		}
		rows[i] = trainingRow{Group: fmt.Sprintf("%t-%d", facing, i), Identity: fmt.Sprintf("%t-%d", facing, i), Weight: 1,
			Features: sixmaxclone.Features{Facing: facing, Position: i % 2}, Label: label, Available: [3]bool{facing, true, true}}
	}
	return rows
}

func TestFitRepresentsSparseContextAsDeferringLeaf(t *testing.T) {
	model, err := fitModel(append(syntheticRows(20, true), syntheticRows(19, false)...))
	if err != nil {
		t.Fatal(err)
	}
	if err := model.Validate(); err != nil {
		t.Fatal(err)
	}
	if groups := model.Trees["checked"].Nodes[0].Leaf.Groups; groups != 19 {
		t.Fatalf("sparse leaf groups = %v, want 19", groups)
	}
	if _, ok := model.Predict(sixmaxclone.Features{}); ok {
		t.Fatal("sparse checked context did not defer")
	}
}

func TestFitRepresentsEmptyContextAsDeferringLeaf(t *testing.T) {
	model, err := fitModel(syntheticRows(20, true))
	if err != nil {
		t.Fatal(err)
	}
	leaf := model.Trees["checked"].Nodes[0].Leaf
	if leaf == nil || leaf.Groups != 0 {
		t.Fatalf("empty context leaf = %+v", leaf)
	}
}

func TestCARTSplitRequiresTwentyGroupsOnBothChildren(t *testing.T) {
	rows := syntheticRows(39, true)
	if _, ok := bestSplit(rows); ok {
		t.Fatal("split created a child with fewer than 20 distinct groups")
	}
	rows = syntheticRows(40, true)
	for i := range rows {
		rows[i].Features.Position = i / 20
	}
	if split, ok := bestSplit(rows); !ok || distinctGroups(split.left) != 20 || distinctGroups(split.right) != 20 {
		t.Fatalf("40-group split groups = %d/%d, ok %t", distinctGroups(split.left), distinctGroups(split.right), ok)
	}
}

func TestDuplicateRowsDoNotChangeFittedModel(t *testing.T) {
	rows := append(syntheticRows(40, true), syntheticRows(40, false)...)
	for i := range rows {
		rows[i].Features.Position = (i % 40) / 20
	}
	deduplicated, err := deduplicateAndWeight(rows)
	if err != nil {
		t.Fatal(err)
	}
	base, err := fitModel(deduplicated)
	if err != nil {
		t.Fatal(err)
	}
	deduplicated, err = deduplicateAndWeight(append(append([]trainingRow(nil), rows...), rows[0]))
	if err != nil {
		t.Fatal(err)
	}
	withDuplicate, err := fitModel(deduplicated)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(base)
	b, _ := json.Marshal(withDuplicate)
	if string(a) != string(b) {
		t.Fatal("duplicate row changed deterministic fitted model")
	}
}
