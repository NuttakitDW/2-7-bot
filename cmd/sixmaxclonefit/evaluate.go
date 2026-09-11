package main

import (
	"fmt"
	"math"

	"github.com/nuttakit/2-7-bot/internal/sixmaxclone"
)

type actionMetric struct {
	Support float64 `json:"support"`
	Recall  float64 `json:"recall"`
}

type thresholdMetrics struct {
	Confidence        float64                  `json:"confidence"`
	Coverage          float64                  `json:"coverage"`
	OverrideRows      int                      `json:"overrideRows"`
	OverrideGroups    int                      `json:"overrideGroups"`
	BaselineAgreement float64                  `json:"baselineAgreement"`
	ModelAgreement    float64                  `json:"modelAgreement"`
	OverrideGain      float64                  `json:"overrideGain"`
	LogLoss           float64                  `json:"logLoss"`
	BaselineRecall    map[string]actionMetric  `json:"baselineRecall"`
	CandidateRecall   map[string]actionMetric  `json:"candidateRecall"`
	Transitions       map[string]float64       `json:"transitions"`
	Eligible          bool                     `json:"eligible"`
	Breakdown         map[string]segmentMetric `json:"breakdown"`
}

type segmentMetric struct {
	Rows              int     `json:"rows"`
	Groups            int     `json:"groups"`
	BaselineAgreement float64 `json:"baselineAgreement"`
	ModelAgreement    float64 `json:"modelAgreement"`
	Gain              float64 `json:"gain"`
}

type segmentAccumulator struct {
	rows                        int
	weight, baseline, candidate float64
	groups                      map[string]bool
}

type fitMetrics struct {
	Schema             int                `json:"schema"`
	Source             string             `json:"source"`
	TrainingRows       int                `json:"trainingRows"`
	TrainingGroups     int                `json:"trainingGroups"`
	ValidationRows     int                `json:"validationRows"`
	ValidationGroups   int                `json:"validationGroups"`
	Sources            []string           `json:"sources"`
	SelectedConfidence float64            `json:"selectedConfidence"`
	Thresholds         []thresholdMetrics `json:"thresholds"`
}

func evaluateThreshold(model *sixmaxclone.Model, rows []trainingRow, confidence float64) thresholdMetrics {
	candidate := *model
	candidate.MinConfidence = confidence
	metric := thresholdMetrics{Confidence: confidence, BaselineRecall: map[string]actionMetric{},
		CandidateRecall: map[string]actionMetric{}, Transitions: map[string]float64{}, Breakdown: map[string]segmentMetric{}}
	segments := map[string]*segmentAccumulator{}
	var total, covered, baselineCorrect, modelCorrect float64
	var overrideWeight, overrideBaselineCorrect, overrideModelCorrect float64
	var nonFoldBaselineCorrect, nonFoldModelCorrect float64
	overrideGroups := map[string]bool{}
	var support, baselineActionCorrect, candidateCorrect [3]float64
	for _, row := range rows {
		total += row.Weight
		if row.Baseline == row.Label {
			baselineCorrect += row.Weight
			baselineActionCorrect[row.Label] += row.Weight
		}
		applied := row.Baseline
		prediction, ok := candidate.Predict(row.Features)
		if ok && row.Available[prediction] {
			covered += row.Weight
			applied = prediction
		}
		if applied == row.Label {
			modelCorrect += row.Weight
		}
		if name := evaluationSegment(row.Features); name != "" {
			segment := segments[name]
			if segment == nil {
				segment = &segmentAccumulator{groups: map[string]bool{}}
				segments[name] = segment
			}
			segment.rows++
			segment.groups[row.Group] = true
			segment.weight += row.Weight
			if row.Baseline == row.Label {
				segment.baseline += row.Weight
			}
			if applied == row.Label {
				segment.candidate += row.Weight
			}
		}
		support[row.Label] += row.Weight
		if applied == row.Label {
			candidateCorrect[row.Label] += row.Weight
		}
		if ok && row.Available[prediction] && prediction != row.Baseline {
			metric.OverrideRows++
			overrideGroups[row.Group] = true
			overrideWeight += row.Weight
			if row.Baseline == row.Label {
				overrideBaselineCorrect += row.Weight
				if row.Label != sixmaxclone.Fold {
					nonFoldBaselineCorrect += row.Weight
				}
			}
			if prediction == row.Label {
				overrideModelCorrect += row.Weight
				if row.Label != sixmaxclone.Fold {
					nonFoldModelCorrect += row.Weight
				}
			}
			metric.Transitions[actionName(row.Baseline)+"->"+actionName(prediction)] += row.Weight
		}
		leaf, leafOK := modelLeaf(model, row.Features)
		if leafOK {
			metric.LogLoss += row.Weight * -math.Log(math.Max(leaf.Probabilities[row.Label], 1e-12))
		}
	}
	metric.OverrideGroups = len(overrideGroups)
	if total > 0 {
		metric.Coverage = covered / total
		metric.BaselineAgreement = baselineCorrect / total
		metric.ModelAgreement = modelCorrect / total
		metric.LogLoss /= total
	}
	if overrideWeight > 0 {
		metric.OverrideGain = (overrideModelCorrect - overrideBaselineCorrect) / overrideWeight
	}
	for action := sixmaxclone.Fold; action <= sixmaxclone.Aggressive; action++ {
		baselineRecall, candidateRecall := 0.0, 0.0
		if support[action] > 0 {
			baselineRecall = baselineActionCorrect[action] / support[action]
			candidateRecall = candidateCorrect[action] / support[action]
		}
		metric.BaselineRecall[actionName(action)] = actionMetric{Support: support[action], Recall: baselineRecall}
		metric.CandidateRecall[actionName(action)] = actionMetric{Support: support[action], Recall: candidateRecall}
	}
	for name, segment := range segments {
		baselineAgreement, modelAgreement := 0.0, 0.0
		if segment.weight > 0 {
			baselineAgreement = segment.baseline / segment.weight
			modelAgreement = segment.candidate / segment.weight
		}
		metric.Breakdown[name] = segmentMetric{Rows: segment.rows, Groups: len(segment.groups),
			BaselineAgreement: baselineAgreement, ModelAgreement: modelAgreement, Gain: modelAgreement - baselineAgreement}
	}
	metric.Eligible = metric.OverrideGroups >= 20 && overrideModelCorrect > overrideBaselineCorrect+1e-12 &&
		nonFoldModelCorrect > nonFoldBaselineCorrect+1e-12
	return metric
}

func evaluationSegment(features sixmaxclone.Features) string {
	switch {
	case features.Aggressions == 0:
		return "opening"
	case features.OwnRaises > 0 && features.Aggressions == 2:
		return "facing-three-bet"
	case features.OwnRaises > 0 && features.Aggressions >= 3:
		return "facing-four-bet"
	default:
		return ""
	}
}

func selectConfidence(model *sixmaxclone.Model, rows []trainingRow) (float64, []thresholdMetrics, error) {
	confidences := [...]float64{.65, .75, .85}
	metrics := make([]thresholdMetrics, 0, len(confidences))
	selected := -1
	for _, confidence := range confidences {
		metrics = append(metrics, evaluateThreshold(model, rows, confidence))
		current := len(metrics) - 1
		if !metrics[current].Eligible {
			continue
		}
		if selected < 0 || metrics[current].OverrideGain > metrics[selected].OverrideGain+1e-12 ||
			(math.Abs(metrics[current].OverrideGain-metrics[selected].OverrideGain) <= 1e-12 && metrics[current].Coverage > metrics[selected].Coverage) {
			selected = current
		}
	}
	if selected < 0 {
		return 0, metrics, fmt.Errorf("no confidence threshold produced a positive non-fold-only gain across at least 20 validation groups")
	}
	return metrics[selected].Confidence, metrics, nil
}

func modelLeaf(model *sixmaxclone.Model, features sixmaxclone.Features) (*sixmaxclone.Leaf, bool) {
	context := "checked"
	if features.Facing {
		context = "facing"
	}
	tree, ok := model.Trees[context]
	if !ok || len(tree.Nodes) == 0 {
		return nil, false
	}
	index := 0
	for steps := 0; steps <= len(tree.Nodes); steps++ {
		node := tree.Nodes[index]
		if node.Leaf != nil {
			return node.Leaf, true
		}
		if features.Value(node.Feature) <= node.Threshold {
			index = node.Left
		} else {
			index = node.Right
		}
	}
	return nil, false
}

func actionName(action sixmaxclone.Action) string {
	return [...]string{"fold", "passive", "aggressive"}[action]
}
