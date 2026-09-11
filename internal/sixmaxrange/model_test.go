package sixmaxrange

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/deuce"
)

func TestDistributionCDFBoundariesAndTies(t *testing.T) {
	d := Distribution{Ranks: []RankMass{{100, .25}, {200, .5}, {300, .25}}}
	for _, tc := range []struct {
		hero        deuce.Value
		less, equal float64
	}{
		{50, 0, 0}, {100, 0, .25}, {200, .25, .5}, {250, .75, 0}, {400, 1, 0},
	} {
		less, equal := d.CDF(tc.hero)
		if less != tc.less || equal != tc.equal {
			t.Errorf("CDF(%d)=(%g,%g), want (%g,%g)", tc.hero, less, equal, tc.less, tc.equal)
		}
	}
	if q := ShowdownEquity(200, []Distribution{d}); math.Abs(q-.5) > 1e-12 {
		t.Fatalf("HU equity = %g", q)
	}
}

func TestFiveOpponentTiePolynomialSplitsOneSixth(t *testing.T) {
	d := Distribution{Ranks: []RankMass{{Value: 777, Mass: 1}}}
	if q := ShowdownEquity(777, []Distribution{d, d, d, d, d}); math.Abs(q-1.0/6.0) > 1e-12 {
		t.Fatalf("five-way tie equity = %.12f", q)
	}
}

func TestFitDeduplicatesRowsAndCountsEffectiveGroups(t *testing.T) {
	c := Context{FinalDraw: DrawOne, LastAction: "bet", ActionFamily: Aggressive}
	rows := []Row{}
	for i := 0; i < 30; i++ {
		rows = append(rows, Row{MatchID: 1, Hand: 1, Group: "same", Target: 2, Context: c, Value: 100, Weight: 1.0 / 6})
	}
	m, err := Fit(rows, FitConfig{FullMinGroups: 1.1, BroadMinGroups: 1.1, Shrink: 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Lookup(c); ok {
		t.Fatal("duplicate rows inflated one group above support")
	}
	rows = append(rows,
		Row{MatchID: 1, Hand: 7, Group: "second", Target: 2, Context: c, Value: 200, Weight: 1.0 / 6})
	m, err = Fit(rows, FitConfig{FullMinGroups: 1.1, BroadMinGroups: 1.1, Shrink: 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Lookup(c); !ok {
		t.Fatal("two independent groups did not meet support")
	}
}

func TestFitDeduplicatesEachHierarchyLevelSeparately(t *testing.T) {
	a := Context{FinalDraw: DrawOne, LastAction: "check", ActionFamily: "check", Multiway: false}
	b := a
	b.Multiway = true
	rows := []Row{{MatchID: 1, Hand: 1, Group: "g1", Target: 2, Context: a, Value: 100, Weight: 1}, {MatchID: 1, Hand: 1, Group: "g1", Target: 2, Context: b, Value: 200, Weight: 1}}
	m, err := Fit(rows, FitConfig{FullMinGroups: .5, BroadMinGroups: .5, Shrink: 0})
	if err != nil {
		t.Fatal(err)
	}
	var parent Cell
	full := 0
	for _, cell := range m.Cells {
		if cell.Level == "action" && cell.Key == "check" {
			parent = cell
		}
		if cell.Level == "full" {
			full++
		}
	}
	if full != 2 {
		t.Fatalf("full cells=%d, want2", full)
	}
	if len(parent.Distribution.Ranks) != 1 || parent.Distribution.Ranks[0].Value != 100 {
		t.Fatalf("shared parent double counted detailed row: %+v", parent)
	}
}

func TestFitRejectsInvalidThresholds(t *testing.T) {
	for _, cfg := range []FitConfig{{0, 20, 20}, {8, -1, 20}, {8, 20, -1}, {8, 20, math.NaN()}} {
		if _, err := Fit(nil, cfg); err == nil {
			t.Fatalf("accepted %+v", cfg)
		}
	}
}

func TestUnsupportedDefersAndInvalidModelsReject(t *testing.T) {
	m := Model{Schema: SchemaVersion, Source: "pooled strong six-max opponents"}
	if _, ok := m.Lookup(Context{}); ok {
		t.Fatal("empty model reported support")
	}
	bad := `{"schema":1,"source":"x","cells":[{"level":"action","key":"a:passive","effectiveGroups":20,"distribution":{"ranks":[{"value":2,"mass":0.8},{"value":1,"mass":0.3}]}}]}`
	if _, err := Decode([]byte(bad)); err == nil {
		t.Fatal("accepted unsorted/non-normalized model")
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(encoded); err != nil {
		t.Fatalf("valid empty model: %v", err)
	}
}

func TestDecodeRejectsMalformedHierarchyAndRank(t *testing.T) {
	for _, raw := range []string{
		`{"schema":1,"source":"x","cells":[{"level":"bogus","key":"none","effectiveGroups":1,"distribution":{"ranks":[{"value":1,"mass":1}]}}]}`,
		`{"schema":1,"source":"x","cells":[{"level":"full","key":"unknown|false|none|false|false","effectiveGroups":1,"distribution":{"ranks":[{"value":1,"mass":1}]}}]}`,
		`{"schema":1,"source":"x","cells":[{"level":"action","key":"none","effectiveGroups":1,"distribution":{"ranks":[{"value":16777216,"mass":1}]}}]}`,
	} {
		if _, err := Decode([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid model %s", raw)
		}
	}
}

func TestPatBackoffUsesFinalPatNotPriorStreetPat(t *testing.T) {
	a := Context{FinalDraw: DrawPat, PriorPat: false, LastAction: "check", ActionFamily: "check"}
	b := Context{FinalDraw: DrawOne, PriorPat: true, LastAction: "check", ActionFamily: "check"}
	if a.key("pat-action") == b.key("pat-action") {
		t.Fatal("final pat and prior pat collapsed")
	}
}

func TestEmbeddedDefaultIsValidAndUnsupported(t *testing.T) {
	m, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Lookup(Context{}); ok {
		t.Fatal("default model should defer")
	}
}

func TestContextUsesOnlyPrefix(t *testing.T) {
	var draws [6][3]int
	for i := range draws {
		draws[i] = [3]int{-1, -1, -1}
	}
	draws[2] = [3]int{2, 0, 1}
	actions := []PublicAction{{Seat: 2, Street: 0, Action: "raise"}, {Seat: 2, Street: 3, Action: "bet"}, {Seat: 3, Street: 3, Action: "check"}}
	got, ok := BuildContext(2, draws, actions, 3)
	if !ok {
		t.Fatal("known draw rejected")
	}
	want := Context{FinalDraw: DrawOne, PriorPat: true, LastAction: "bet", Multiway: true, ActionFamily: Aggressive}
	if got != want {
		t.Fatalf("context = %+v, want %+v", got, want)
	}
	actions = append(actions, PublicAction{Seat: 2, Street: 3, Action: "raise"}, PublicAction{Seat: 4, Street: 3, Action: "call"})
	got, ok = BuildContext(2, draws, actions, 3)
	if !ok {
		t.Fatal("known draw rejected")
	}
	if got.LastAction != "raise" || !got.RaisedEarlier {
		t.Fatalf("target river history lost behind other action: %+v", got)
	}
	draws[5][2] = 0
	untouched, ok := BuildContext(5, draws, actions, 3)
	if !ok {
		t.Fatal("known pat rejected")
	}
	if untouched.LastAction != "none" || untouched.RaisedEarlier {
		t.Fatalf("other target inherited action: %+v", untouched)
	}
}

func TestContextRejectsUnknownFinalDraw(t *testing.T) {
	var draws [6][3]int
	for i := range draws {
		draws[i] = [3]int{-1, -1, -1}
	}
	if _, ok := BuildContext(2, draws, nil, 2); ok {
		t.Fatal("unknown final draw was bucketed")
	}
}

func BenchmarkProductionFiveOpponentLookupsAndPolynomial(b *testing.B) {
	path := os.Getenv("SIXMAX_RANGE_MODEL")
	if path == "" {
		b.Skip("set SIXMAX_RANGE_MODEL")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	m, err := Decode(raw)
	if err != nil {
		b.Fatal(err)
	}
	contexts := []Context{
		{FinalDraw: DrawOne, LastAction: "none", ActionFamily: "none", Multiway: true},
		{FinalDraw: DrawOne, LastAction: "check", ActionFamily: "check", Multiway: true},
		{FinalDraw: DrawOne, LastAction: "bet", ActionFamily: Aggressive, Multiway: true},
		{FinalDraw: DrawPat, LastAction: "none", ActionFamily: "none", Multiway: true},
		{FinalDraw: DrawPat, LastAction: "bet", ActionFamily: Aggressive, Multiway: true},
	}
	opponents := make([]Distribution, len(contexts))
	for i, ctx := range contexts {
		d, ok := m.Lookup(ctx)
		if !ok {
			b.Fatalf("production context unsupported: %+v", ctx)
		}
		opponents[i] = d
	}
	hero := opponents[0].Ranks[len(opponents[0].Ranks)/2].Value
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j, ctx := range contexts {
			opponents[j], _ = m.Lookup(ctx)
		}
		_ = ShowdownEquity(hero, opponents)
	}
}
