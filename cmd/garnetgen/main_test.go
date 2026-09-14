package main

import (
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/garnet"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/lapis"
)

func TestSamplePhysicalClassCoversEveryFeasibleClass(t *testing.T) {
	rng := rand.New(rand.NewPCG(41, 97))
	ids := feasibleClasses()
	if len(ids) == 0 {
		t.Fatal("no feasible classes")
	}
	weight := 0
	for _, id := range ids {
		weight += handclass.Weight(id)
		for sample := 0; sample < 4; sample++ {
			hand := samplePhysicalClass(id, rng)
			if len(hand) != 5 || cards.NewSet(hand).Len() != 5 {
				t.Fatalf("class %d sampled invalid physical hand %v", id, cards.Strings(hand))
			}
			if got := handclass.Of(hand); got != id {
				t.Fatalf("class %d sampled as class %d: %v", id, got, cards.Strings(hand))
			}
			if cards.SameSuit(hand) != cards.SameSuit(handclass.Representative(id)) {
				t.Fatalf("class %d changed flush status: %v", id, cards.Strings(hand))
			}
		}
	}
	if weight != cards.Deals {
		t.Fatalf("feasible physical weight=%d want=%d", weight, cards.Deals)
	}
}

func TestSamplePhysicalClassVariesSuitRealizations(t *testing.T) {
	var mixed, flush handclass.ID
	for _, id := range feasibleClasses() {
		representative := handclass.Representative(id)
		if cards.SameSuit(representative) && flush == 0 {
			flush = id
		}
		if !cards.SameSuit(representative) && handclass.Weight(id) >= 100 && mixed == 0 {
			mixed = id
		}
	}
	for name, id := range map[string]handclass.ID{"mixed": mixed, "flush": flush} {
		rng := rand.New(rand.NewPCG(101, 303))
		seen := map[string]bool{}
		for i := 0; i < 64; i++ {
			seen[fmt.Sprint(cards.Strings(samplePhysicalClass(id, rng)))] = true
		}
		if len(seen) < 2 {
			t.Fatalf("%s class %d never varied suits", name, id)
		}
	}
}

func TestMomentsProducesSampleMeanAndStandardError(t *testing.T) {
	var got moments
	for _, value := range []float64{1, 2, 3, 4} {
		got.add(value)
	}
	estimate := got.estimate()
	wantSE := math.Sqrt((5.0 / 3.0) / 4.0)
	if estimate.Count != 4 || estimate.Mean != 2.5 || math.Abs(estimate.SE-wantSE) > 1e-12 {
		t.Fatalf("estimate=%+v want count=4 mean=2.5 se=%g", estimate, wantSE)
	}
	var singleton moments
	singleton.add(9)
	if got := singleton.estimate(); got != (garnet.Estimate{Mean: 9, Count: 1, SE: 0}) {
		t.Fatalf("singleton estimate=%+v", got)
	}
}

func TestTinyClassEstimateIsReproducibleAndPaired(t *testing.T) {
	id := feasibleClasses()[len(feasibleClasses())/3]
	a, statsA, err := estimateClass(id, 2, 27091501, cfr.Heuristic{})
	if err != nil {
		t.Fatal(err)
	}
	b, statsB, err := estimateClass(id, 2, 27091501, cfr.Heuristic{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(statsA, statsB) {
		t.Fatalf("same seed diverged:\n%+v %+v\n%+v %+v", a, statsA, b, statsB)
	}
	if a.Call.Count != 12 || a.Raise.Count != 12 || a.BestEV != max(a.Call.Mean, a.Raise.Mean) {
		t.Fatalf("unpaired estimate: %+v", a)
	}
	var paired uint64
	for _, byContext := range statsA.PairedSamples {
		for _, count := range byContext {
			paired += count
		}
	}
	if paired != 12 {
		t.Fatalf("paired sample count=%d want=12", paired)
	}
}

func TestScaledPercentages(t *testing.T) {
	got, err := scaledPercentages("", 0.8)
	if err != nil {
		t.Fatal(err)
	}
	if got[garnet.UnderTheGun][garnet.Unopened] != 16 || got[garnet.Button][garnet.Unopened] != 36 {
		t.Fatalf("scaled defaults UTG=%g BTN=%g", got[garnet.UnderTheGun][garnet.Unopened], got[garnet.Button][garnet.Unopened])
	}
	custom := `[[10,0,0,0],[0,0,0,0],[0,0,0,0],[0,0,0,0],[0,0,0,0],[0,0,0,0]]`
	got, err = scaledPercentages(custom, 1.2)
	if err != nil || got[garnet.Button][garnet.Unopened] != 12 {
		t.Fatalf("custom scaled percentages=%v err=%v", got, err)
	}
	if _, err := scaledPercentages("", -1); err == nil {
		t.Fatal("accepted negative scale")
	}
	if _, err := scaledPercentages("[] trailing", 1); err == nil {
		t.Fatal("accepted malformed percentages")
	}
	for _, malformed := range []string{
		`[[1,2,3,4]]`,
		`[[1,2,3,4,5],[0,0,0,0],[0,0,0,0],[0,0,0,0],[0,0,0,0],[0,0,0,0]]`,
		`[[101,0,0,0],[0,0,0,0],[0,0,0,0],[0,0,0,0],[0,0,0,0],[0,0,0,0]]`,
	} {
		if _, err := scaledPercentages(malformed, 1); err == nil {
			t.Fatalf("accepted invalid percentages %s", malformed)
		}
	}
}

func TestReferencePolicyHashMustMatchEmbeddedSpinel(t *testing.T) {
	hash, err := lapis.EmbeddedPolicyHash()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateReferencePolicyHash(hash); err != nil {
		t.Fatalf("rejected matching hash: %v", err)
	}
	if err := validateReferencePolicyHash(strings.Repeat("0", 64)); err == nil {
		t.Fatal("accepted mismatched reference policy hash")
	}
}
