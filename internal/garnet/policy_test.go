package garnet

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/beryl"
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestBucketerUsesPhysicalWeightedQuantiles(t *testing.T) {
	ranking := testRanking(t, func(id handclass.ID) float64 { return float64(id) })
	b, err := NewBucketer(ranking, 16)
	if err != nil {
		t.Fatal(err)
	}
	counts := [16]int{}
	for deal := 0; deal < cards.Deals; deal++ {
		bucket, ok := b.Bucket(cards.DealFromIndex(deal).Append(nil))
		if !ok {
			t.Fatalf("deal %d rejected", deal)
		}
		counts[bucket]++
	}
	for bucket, count := range counts {
		wantLow := cards.Deals / 16
		if count < wantLow || count > wantLow+1 {
			t.Fatalf("bucket %d physical weight=%d", bucket, count)
		}
	}
}

func TestCanonicalKeyIgnoresCardsAndMatchesButtonRotations(t *testing.T) {
	base := beryl.PredrawView{
		Hand: cards.MustParse("2c", "3d", "5h", "7s", "Kc"), Position: beryl.Cutoff,
		Active: 0x3f, Wagers: 2, Commitments: [6]uint64{0, 50, 100, 0, 0, 200},
		Actions: []beryl.PredrawAction{{Position: beryl.Cutoff, Action: wire.ActionRaise, Commit: 200}},
	}
	otherCards := base
	otherCards.Hand = cards.MustParse("4c", "6d", "8h", "Ts", "Ac")
	if KeyFor(base, 7) != KeyFor(otherCards, 7) {
		t.Fatal("private cards leaked into public infoset key")
	}
	if KeyFor(base, 6) == KeyFor(base, 7) {
		t.Fatal("private EV bucket missing from infoset key")
	}
}

func TestPolicyRoundTripAndLookupCoverage(t *testing.T) {
	ranking := testRanking(t, func(id handclass.ID) float64 { return float64(id) })
	rankingHash, err := RankingHash(ranking)
	if err != nil {
		t.Fatal(err)
	}
	key := KeyFor(beryl.PredrawView{Position: beryl.Button, Active: 0x3f}, 1)
	p := &Policy{Version: PolicyVersion, Algorithm: "external-sampling-cfr+", RankingHash: rankingHash, Percentages: DefaultPercentages,
		BucketCount: 16, Seed: 9, Iterations: 10, Infosets: map[string]Strategy{key: {Passive: .25, Raise: .75, Visits: 3}}}
	p.PercentagesHash, _ = PercentagesHash(p.Percentages)
	p.Backoff[Button][Unopened] = make([]Strategy, 16)
	p.Backoff[Button][Unopened][2] = Strategy{Passive: .8, Raise: .2, Visits: 2}
	raw, err := EncodePolicy(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodePolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strategy, kind := got.Lookup(key, Button, Unopened, 1); kind != LookupExact || strategy.Raise != .75 {
		t.Fatalf("exact lookup=%+v %v", strategy, kind)
	}
	if strategy, kind := got.Lookup("missing", Button, Unopened, 2); kind != LookupBackoff || strategy.Passive != .8 {
		t.Fatalf("backoff lookup=%+v %v", strategy, kind)
	}
	if strategy, kind := got.Lookup("missing", UnderTheGun, MultipleOpen, 15); kind != LookupUntrained || strategy.Passive != 1 {
		t.Fatalf("untrained lookup=%+v %v", strategy, kind)
	}

	got.RankingHash = "stale"
	if err := got.Validate(rankingHash); err == nil {
		t.Fatal("accepted stale ranking binding")
	}
	got.RankingHash = rankingHash
	got.BucketCount = 17
	if err := got.Validate(rankingHash); err == nil {
		t.Fatal("accepted unsupported bucket count")
	}
	if _, err := DecodePolicy(append(raw, []byte(" trailing")...)); err == nil {
		t.Fatal("accepted policy trailing data")
	}
}

func TestCanonicalKeyDistinguishesEveryPublicAction(t *testing.T) {
	seen := map[string]bool{}
	for _, action := range []string{wire.ActionFold, wire.ActionCheck, wire.ActionCall, wire.ActionBet, wire.ActionRaise, "future-action"} {
		view := beryl.PredrawView{Position: beryl.Button, Active: 0x3f,
			Actions: []beryl.PredrawAction{{Position: beryl.Hijack, Action: action, Commit: 200}}}
		key := KeyFor(view, 3)
		if seen[key] {
			t.Fatalf("action %q collided", action)
		}
		seen[key] = true
	}
}
