package main

import (
	"math"
	"sort"

	"github.com/nuttakit/2-7-bot/internal/sixmaxclone"
)

const (
	maximumDepth    = 4
	minimumGroups   = 20
	parentPriorUnit = 1.0
)

type splitCandidate struct {
	feature   int
	threshold float64
	gain      float64
	left      []trainingRow
	right     []trainingRow
}

func fitModel(rows []trainingRow) (*sixmaxclone.Model, error) {
	model := &sixmaxclone.Model{Schema: sixmaxclone.SchemaVersion, Source: "swit2-swit3-grouped-cart",
		MinGroups: minimumGroups, MinConfidence: .65, MinMargin: 0, Trees: map[string]sixmaxclone.Tree{}}
	for _, context := range []struct {
		name   string
		facing bool
	}{{"facing", true}, {"checked", false}} {
		var subset []trainingRow
		for _, row := range rows {
			if row.Features.Facing == context.facing {
				subset = append(subset, row)
			}
		}
		subset = normalizeGroupWeights(subset)
		builder := treeBuilder{}
		rootDistribution := naturalDistribution(subset)
		builder.add(subset, 0, rootDistribution)
		model.Trees[context.name] = sixmaxclone.Tree{Nodes: builder.nodes}
	}
	if err := model.Validate(); err != nil {
		return nil, err
	}
	return model, nil
}

type treeBuilder struct {
	nodes []sixmaxclone.Node
}

func (b *treeBuilder) add(rows []trainingRow, depth int, parent [3]float64) int {
	index := len(b.nodes)
	b.nodes = append(b.nodes, sixmaxclone.Node{})
	distribution := smoothedDistribution(rows, parent)
	split, ok := bestSplit(rows)
	if depth >= maximumDepth || !ok {
		b.nodes[index] = sixmaxclone.Node{Leaf: &sixmaxclone.Leaf{Groups: float64(distinctGroups(rows)), Probabilities: distribution}}
		return index
	}
	left := b.add(split.left, depth+1, distribution)
	right := b.add(split.right, depth+1, distribution)
	b.nodes[index] = sixmaxclone.Node{Feature: split.feature, Threshold: split.threshold, Left: left, Right: right}
	return index
}

func bestSplit(rows []trainingRow) (splitCandidate, bool) {
	parentWeight := totalWeight(rows)
	parentImpurity := gini(classWeights(rows))
	best := splitCandidate{gain: 1e-12}
	found := false
	for feature := 0; feature < sixmaxclone.FeatureCount; feature++ {
		values := make([]int, 0, len(rows))
		seen := map[int]bool{}
		for _, row := range rows {
			value := int(row.Features.Value(feature))
			if !seen[value] {
				seen[value] = true
				values = append(values, value)
			}
		}
		sort.Ints(values)
		for i := 0; i+1 < len(values); i++ {
			low, high := float64(values[i]), float64(values[i+1])
			threshold := low + (high-low)/2
			left, right := partition(rows, feature, threshold)
			if distinctGroups(left) < minimumGroups || distinctGroups(right) < minimumGroups {
				continue
			}
			leftWeight, rightWeight := totalWeight(left), totalWeight(right)
			gain := parentImpurity - leftWeight/parentWeight*gini(classWeights(left)) - rightWeight/parentWeight*gini(classWeights(right))
			if gain > best.gain+1e-12 || (!found && gain >= best.gain) {
				best = splitCandidate{feature: feature, threshold: threshold, gain: gain, left: left, right: right}
				found = true
			}
		}
	}
	return best, found
}

func partition(rows []trainingRow, feature int, threshold float64) (left, right []trainingRow) {
	for _, row := range rows {
		if row.Features.Value(feature) <= threshold {
			left = append(left, row)
		} else {
			right = append(right, row)
		}
	}
	return
}

func normalizeGroupWeights(rows []trainingRow) []trainingRow {
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.Group]++
	}
	normalized := append([]trainingRow(nil), rows...)
	for i := range normalized {
		normalized[i].Weight = 1 / float64(counts[normalized[i].Group])
	}
	return normalized
}

func distinctGroups(rows []trainingRow) int {
	groups := map[string]struct{}{}
	for _, row := range rows {
		groups[row.Group] = struct{}{}
	}
	return len(groups)
}

func totalWeight(rows []trainingRow) float64 {
	total := 0.0
	for _, row := range rows {
		total += row.Weight
	}
	return total
}

func classWeights(rows []trainingRow) [3]float64 {
	var weights [3]float64
	for _, row := range rows {
		weights[row.Label] += row.Weight
	}
	return weights
}

func naturalDistribution(rows []trainingRow) [3]float64 {
	weights := classWeights(rows)
	total := weights[0] + weights[1] + weights[2]
	if total == 0 {
		return [3]float64{1.0 / 3, 1.0 / 3, 1.0 / 3}
	}
	for i := range weights {
		weights[i] /= total
	}
	return weights
}

func smoothedDistribution(rows []trainingRow, parent [3]float64) [3]float64 {
	weights := classWeights(rows)
	total := weights[0] + weights[1] + weights[2]
	for i := range weights {
		weights[i] = (weights[i] + parentPriorUnit*parent[i]) / (total + parentPriorUnit)
	}
	return weights
}

func gini(weights [3]float64) float64 {
	total := weights[0] + weights[1] + weights[2]
	if total == 0 {
		return 0
	}
	impurity := 1.0
	for _, weight := range weights {
		p := weight / total
		impurity -= p * p
	}
	return math.Max(0, impurity)
}
