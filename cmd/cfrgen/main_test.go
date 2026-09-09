package main

import (
	"strings"
	"testing"
)

func TestTrainRejectsInvalidSettings(t *testing.T) {
	for _, args := range [][]string{
		{"-fixed-only", "-init-blueprint", "prior.gz", "-fixed", "random", "-minvisits", "20"},
		{"-fixed-only"}, {"-fixed-only", "-fixed", "random"}, {"-fixed-only", "-init-blueprint", "prior.gz"},
		{"-iters", "0"}, {"-workers", "0"}, {"-every", "0s"},
		{"-weight", "-1"}, {"-weight", "1.1"}, {"-weight", "NaN"},
		{"-weight", "1"},
		{"-regret", "invalid"},
		{"-regret", "discounted", "-discount-every", "0"},
		{"-regret", "discounted", "-resume", "old.state"},
		{"-init-blueprint", "prior.gz", "-resume", "old.state"},
		{"-init-blueprint", "prior.gz", "-prior-strength", "0"},
		{"-init-blueprint", "prior.gz", "-prior-strength", "NaN"},
		{"-model-scope", "invalid"},
	} {
		if err := train(args); err == nil {
			t.Errorf("train(%v) accepted invalid settings", args)
		}
	}
}

func TestEvalRejectsInvalidSettings(t *testing.T) {
	for _, args := range [][]string{
		{}, {"-bp", "h3", "-vs", ""}, {"-bp", "h3", "-hands", "0"},
		{"-bp", "h3", "-purify", "-1"}, {"-bp", "h3", "-purify", "NaN"},
		{"-bp", "h3", "-purify", "1.1"},
	} {
		if err := eval(args); err == nil {
			t.Errorf("eval(%v) accepted invalid settings", args)
		}
	}
}

func TestFrozenRolloutAndResumeValidation(t *testing.T) {
	for _, args := range [][]string{
		{"-frozen-rollouts"},
		{"-fixed-only", "-resume", "old.state", "-fixed", "random", "-minvisits", "1", "-resetavg"},
	} {
		err := train(args)
		if err == nil || (!strings.Contains(err.Error(), "frozen-rollouts requires") && !strings.Contains(err.Error(), "fixed-only requires")) {
			t.Fatalf("incorrect validation for %v: %v", args, err)
		}
	}
}

func TestTrainAverageSamplingValidation(t *testing.T) {
	for _, args := range [][]string{
		{"-average-every", "0"}, {"-average-every", "-1"},
		{"-average-every", "10"},
		{"-average-every", "10", "-model", "h3", "-weight", "0"},
	} {
		err := train(args)
		if err == nil || !strings.Contains(err.Error(), "average-every") || strings.Contains(err.Error(), "not defined") {
			t.Fatalf("incorrect sampling validation for %v: %v", args, err)
		}
	}
}

func TestTrainModelDrawSamplingValidation(t *testing.T) {
	for _, args := range [][]string{
		{"-sample-model-draws"},
		{"-sample-model-draws", "-model", "h3", "-weight", ".25"},
		{"-sample-model-draws", "-model", "h3", "-weight", "0", "-model-scope", "hand"},
	} {
		err := train(args)
		if err == nil || !strings.Contains(err.Error(), "sample-model-draws requires") {
			t.Fatalf("incorrect sampling validation for %v: %v", args, err)
		}
	}
}

func TestTrainUniformModelDrawValidation(t *testing.T) {
	err := train([]string{"-uniform-model-draws", "-model", "h3", "-weight", ".25", "-model-scope", "hand"})
	if err == nil || !strings.Contains(err.Error(), "uniform-model-draws requires sample-model-draws") {
		t.Fatalf("incorrect validation: %v", err)
	}
}

func TestTrainCompressionValidation(t *testing.T) {
	for _, level := range []string{"0", "10", "-1"} {
		err := train([]string{"-compression", level})
		if err == nil || !strings.Contains(err.Error(), "compression must be between 1 and 9") {
			t.Fatalf("compression %s: %v", level, err)
		}
	}
}

func TestTrainDrawBaselineValidation(t *testing.T) {
	err := train([]string{"-draw-baseline"})
	if err == nil || !strings.Contains(err.Error(), "draw-baseline requires sample-model-draws") {
		t.Fatalf("incorrect validation: %v", err)
	}
}
