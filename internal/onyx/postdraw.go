package onyx

// Fixed per artifact, independently of the pre-draw profiles and range data.
var snowProfile = "baseline"

var drawProfile = "baseline"

func finalSnowFrequency(current bool) float64 {
	switch snowProfile {
	case "none":
		return 0
	case "balanced":
		if current {
			return .35
		}
		return .15
	default:
		if current {
			return .90
		}
		return .45
	}
}

func secondSnowFrequency() float64 {
	switch snowProfile {
	case "none":
		return 0
	case "balanced":
		return .10
	default:
		return .30
	}
}
