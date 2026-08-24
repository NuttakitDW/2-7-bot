package arena

// Scaled-integer helpers.
//
// The arena transports money, rates and frequencies as scaled integers, and the
// scale is encoded in the field-name suffix. Printing a raw value is wrong by
// three or six orders of magnitude, so every read goes through one of these.
// See docs/arena/http-api.md.

// MilliScale divides …Milli fields (ratePer100Milli, potMilli, netMilli,
// awardsMilli, amountMilli, aggressionMilli, confidence95Milli).
const MilliScale = 1000

// PpmScale divides …Ppm fields (vpipPpm, pfrPpm, foldRatePpm, and the
// showdown frequencies).
const PpmScale = 1_000_000

// Milli converts a …Milli field to its natural unit — big bets, big blinds or
// points, depending on the match's rateUnit.
func Milli(value int64) float64 {
	return float64(value) / MilliScale
}

// Fraction converts a …Ppm field to a 0..1 fraction.
func Fraction(value int64) float64 {
	return float64(value) / PpmScale
}

// Percent converts a …Ppm field to a 0..100 percentage.
func Percent(value int64) float64 {
	return Fraction(value) * 100
}

// Millis converts a …Micros field to milliseconds.
func Millis(micros int64) float64 {
	return float64(micros) / 1000
}
