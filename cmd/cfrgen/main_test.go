package main

import "testing"

func TestTrainRejectsInvalidSettings(t *testing.T) {
	for _, args := range [][]string{
		{"-iters", "0"}, {"-workers", "0"}, {"-every", "0s"},
		{"-weight", "-1"}, {"-weight", "1.1"}, {"-weight", "NaN"},
		{"-weight", "1"},
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
