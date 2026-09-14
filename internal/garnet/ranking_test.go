package garnet

import (
	"math"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/handclass"
)

func TestRankingRoundTripValidatesWeightsAndMetadata(t *testing.T) {
	ranking := testRanking(t, func(id handclass.ID) float64 { return -float64(id) })
	raw, err := EncodeRanking(ranking)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeRanking(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Seed != ranking.Seed || got.ReferencePolicyHash != ranking.ReferencePolicyHash || len(got.Classes) != len(ranking.Classes) {
		t.Fatalf("round trip lost metadata: %+v", got)
	}

	bad := *ranking
	bad.Classes = append([]ClassEV(nil), ranking.Classes...)
	bad.Classes[0].Weight++
	if _, err := EncodeRanking(&bad); err == nil {
		t.Fatal("accepted incorrect physical handclass weight")
	}
}

func TestPercentileGateHandlesTiesAndFractionalBoundaryExactly(t *testing.T) {
	// Every class ties, so class ID is the required deterministic tiebreak.
	ranking := testRanking(t, func(handclass.ID) float64 { return 1 })
	firstWeight := ranking.Classes[0].Weight
	target := max(1, firstWeight/2)
	pct := 100 * float64(target) / float64(cards.Deals)
	var percentages Percentages
	percentages[UnderTheGun][Unopened] = pct
	gate, err := NewGate(ranking, percentages)
	if err != nil {
		t.Fatal(err)
	}
	if got := gate.AdmittedDeals(UnderTheGun, Unopened); got != target {
		t.Fatalf("admitted deals = %d, want exact fractional cutoff %d", got, target)
	}
	id := handclass.ID(ranking.Classes[0].ID)
	fraction := gate.ClassFraction(UnderTheGun, Unopened, id)
	if want := float64(target) / float64(firstWeight); math.Abs(fraction-want) > 1e-12 {
		t.Fatalf("boundary fraction = %.12f, want %.12f", fraction, want)
	}
	if gate.ClassFraction(UnderTheGun, Unopened, handclass.ID(ranking.Classes[1].ID)) != 0 {
		t.Fatal("tie ordering did not use stable lower class ID")
	}
	admitted, seen := 0, 0
	for deal := 0; deal < cards.Deals; deal++ {
		hand := cards.DealFromIndex(deal).Append(nil)
		if handclass.Of(hand) != id {
			continue
		}
		seen++
		if gate.Admit(UnderTheGun, Unopened, hand) {
			admitted++
		}
	}
	if seen != firstWeight || admitted != target {
		t.Fatalf("physical boundary admitted %d/%d, want %d/%d", admitted, seen, target, firstWeight)
	}
}

func TestPhysicalOrdinalCoversDistinctNonflushClass(t *testing.T) {
	seen := make([]bool, 1020)
	ranks := []cards.Rank{cards.Two, cards.Three, cards.Four, cards.Five, cards.Seven}
	for code := 0; code < 1<<10; code++ {
		hand, rest := make([]cards.Card, 5), code
		for i, rank := range ranks {
			hand[i] = cards.Card{Rank: rank, Suit: []cards.Suit{cards.Clubs, cards.Diamonds, cards.Hearts, cards.Spades}[rest%4]}
			rest /= 4
		}
		if cards.SameSuit(hand) {
			continue
		}
		ordinal, ok := physicalOrdinal(hand)
		if !ok || ordinal < 0 || ordinal >= len(seen) || seen[ordinal] {
			t.Fatalf("invalid/repeated ordinal %d for %v", ordinal, hand)
		}
		seen[ordinal] = true
	}
	for ordinal, ok := range seen {
		if !ok {
			t.Fatalf("missing ordinal %d", ordinal)
		}
	}
}

func TestPercentileGateZeroAndHundred(t *testing.T) {
	ranking := testRanking(t, func(id handclass.ID) float64 { return float64(id) })
	var percentages Percentages
	percentages[Button][Unopened] = 100
	gate, err := NewGate(ranking, percentages)
	if err != nil {
		t.Fatal(err)
	}
	if gate.AdmittedDeals(UnderTheGun, Unopened) != 0 || gate.AdmittedDeals(Button, Unopened) != cards.Deals {
		t.Fatalf("0/100 cutoffs = %d/%d", gate.AdmittedDeals(UnderTheGun, Unopened), gate.AdmittedDeals(Button, Unopened))
	}
	for id := handclass.ID(0); id < handclass.Num; id++ {
		if handclass.Weight(id) == 0 {
			continue
		}
		if gate.ClassFraction(Button, Unopened, id) != 1 {
			t.Fatalf("class %d missing at 100%%", id)
		}
	}
}

func testRanking(t *testing.T, score func(handclass.ID) float64) *Ranking {
	t.Helper()
	r := &Ranking{Version: RankingVersion, Seed: 270914, ReferencePolicyHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ReferenceCodeHash: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", SamplesPerPosition: 10,
		EngineHash: "sixmaxsim-v1", ContextMix: "uniform-reached-first-decision", PositionAveraged: true}
	for id := handclass.ID(0); id < handclass.Num; id++ {
		weight := handclass.Weight(id)
		if weight == 0 {
			continue
		}
		mean := score(id)
		r.Classes = append(r.Classes, ClassEV{ID: uint16(id), Weight: weight,
			Call: Estimate{Mean: mean, Count: 10, SE: .1}, Raise: Estimate{Mean: mean - 1, Count: 10, SE: .1}, BestEV: mean})
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	return r
}
