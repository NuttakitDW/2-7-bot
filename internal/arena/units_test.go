package arena

import "testing"

// Values captured from the live API for match 86 (nutt-badugi-fl vs
// rand/30/30/40) on 2026-08-22, so the scales are pinned to real data rather
// than to an assumption about them.
func TestScaledIntegerConversions(t *testing.T) {
	tests := []struct {
		name string
		got  float64
		want float64
	}{
		{"ratePer100Milli to BB/100", Milli(119635), 119.635},
		{"negative rate", Milli(-119635), -119.635},
		{"confidence95Milli", Milli(3831), 3.831},
		{"potMilli", Milli(23000), 23},
		{"netMilli", Milli(-11500), -11.5},
		{"aggressionMilli", Milli(2294), 2.294},
		{"vpipPpm as percent", Percent(494600), 49.46},
		{"foldRatePpm as percent", Percent(165200), 16.52},
		{"wonAtShowdownPpm as fraction", Fraction(741987), 0.741987},
		{"meanMicros to ms", Millis(1106), 1.106},
		{"maxMicros to ms", Millis(2395), 2.395},
		{"zero", Milli(0), 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if diff := test.got - test.want; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("got %v, want %v", test.got, test.want)
			}
		})
	}
}
