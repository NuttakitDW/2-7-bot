package cfr

import (
	"bytes"
	"math"
	"math/rand/v2"
	"reflect"
	"sync"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

func responseView(hand ...string) View {
	v := View{Seat: BB, Street: Draw3, CanRaise: true, LastAggr: Btn, Pot: 800}
	copy(v.Hand[:], cards.SortedByRank(cards.MustParse(hand...)))
	v.Drawn = [2]DrawCounts{{-1, 2, 1, 1}, {-1, 2, 1, 0}}
	return v
}

func TestRiverResponseAbstractionRetainsStrongRanksAndHistory(t *testing.T) {
	a := responseView("2c", "3d", "4h", "6s", "8c")
	b := responseView("2d", "3h", "5s", "6c", "8d")
	if riverHandBucket(a.Hand, "rank") == riverHandBucket(b.Hand, "rank") {
		t.Fatal("distinct strong lows were merged")
	}
	b = responseView("2d", "3h", "4s", "6c", "8d")
	if riverHandBucket(a.Hand, "rank") != riverHandBucket(b.Hand, "rank") {
		t.Fatal("irrelevant suit labels split a low hand")
	}
	tree := BuildTree()
	contexts := newRiverContexts(tree)
	var roots []int32
	for i, n := range tree.Nodes {
		if n.Kind == KindBet && n.Street == Draw3 && n.Actor == BB && n.Wagers == 0 {
			roots = append(roots, int32(i))
		}
	}
	a.Node, b.Node = roots[0], roots[0]
	key := riverKey(&a, contexts, "rank")
	b.Drawn[Btn][Draw2] = 0
	if key == riverKey(&b, contexts, "rank") {
		t.Fatal("lost opponent's prior draw count")
	}
	b = a
	b.LastAggr = BB
	if key == riverKey(&b, contexts, "rank") {
		t.Fatal("lost prior aggressor")
	}
}

func TestRiverResponseLearnsHiddenHandMixtureWithoutPeeking(t *testing.T) {
	// Calling wins 200 in one quarter of samples and loses 200 otherwise;
	// folding loses 50. A clairvoyant maximization would incorrectly call.
	tree := &Tree{Root: 0, Nodes: []Node{
		{Kind: KindBet, Street: Draw3, Actor: Btn, Facing: true, Acts: []uint8{Fold, Pass}, Next: [3]int32{1, 2}},
		{Kind: KindFold, Street: Draw3, Actor: Btn, Commit: [2]int32{50, 200}},
		{Kind: KindShowdown, Street: Draw3, Commit: [2]int32{200, 200}},
	}}
	tr := NewRiverTrainer(tree, passiveModel{}, passiveModel{}, nil, "rank")
	w := riverWorker{tr: tr, rng: rand.New(rand.NewPCG(7, 1))}
	v := responseView("2c", "3d", "4h", "6s", "8c")
	w.state.Hands[Btn] = v.Hand
	w.state.Drawn = v.Drawn
	w.state.LastAggr = -1
	for i := 1; i <= 4000; i++ {
		winner := BB
		if i%4 == 0 {
			winner = Btn
		}
		w.walk(0, Btn, winner, 1, float64(i))
	}
	policy := tr.Extract(10)
	if len(policy.Rows) != 1 || policy.Rows[0].P[Fold] < 250 {
		t.Fatalf("fold should approach certainty, got %+v", policy.Rows)
	}
	model := NewRiverResponse(passiveModel{}, policy, tree)
	v.Seat, v.Node, v.Facing, v.CanRaise, v.Pot, v.LastAggr = Btn, 0, true, false, 0, -1
	for _, random := range []float64{0, .5, .95} {
		v.Rand = random
		if action, ok := model.Bet(&v); !ok || action != Fold {
			t.Fatalf("response = %d,%v at %g, want fold", action, ok, random)
		}
	}
}

func TestRiverResponseSerializationAndFallback(t *testing.T) {
	tree := BuildTree()
	ctx := newRiverContexts(tree)
	v := responseView("2c", "3d", "4h", "6s", "8c")
	for i, n := range tree.Nodes {
		if n.Kind == KindBet && n.Street == Draw3 && n.Actor == BB && n.Wagers == 0 {
			v.Node = int32(i)
			break
		}
	}
	p := &RiverPolicy{Version: 1, Abstraction: "rank", Rows: []RiverRow{{Key: riverKey(&v, ctx, "rank"), P: [3]uint8{0, 0, 255}, Visits: 123}}}
	var b bytes.Buffer
	if err := p.Encode(&b); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeRiverPolicy(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	r := NewRiverResponse(passiveModel{}, got, tree)
	if a, _ := r.Bet(&v); a != Aggr {
		t.Fatalf("action %d", a)
	}
	v.Street = Draw2
	if a, _ := r.Bet(&v); a != Pass {
		t.Fatal("changed frozen street")
	}
	if keep, _ := r.Draw(&v); keep != 31 {
		t.Fatal("changed frozen draw")
	}
	for _, row := range got.Rows {
		sum := 0.0
		for _, x := range row.P {
			sum += float64(x) / 255
		}
		if math.Abs(sum-1) > 1e-9 {
			t.Fatal("not normalized")
		}
	}
}

func TestRiverResponseExposesItsEmpiricalBase(t *testing.T) {
	base := &Empirical{Version: 1}
	r := NewRiverResponse(base.Mode(), &RiverPolicy{Version: 1, Abstraction: "rank"}, BuildTree())
	got, ok := r.EmpiricalBase()
	if !ok || got != base {
		t.Fatalf("EmpiricalBase() = %p, %v; want %p, true", got, ok, base)
	}
	other := NewRiverResponse(passiveModel{}, &RiverPolicy{Version: 1, Abstraction: "rank"}, BuildTree())
	if got, ok := other.EmpiricalBase(); ok || got != nil {
		t.Fatalf("non-empirical base = %p, %v", got, ok)
	}
}

func TestRiverResponseContextsNeverMergeDifferentRiverGames(t *testing.T) {
	tree := BuildTree()
	contexts := newRiverContexts(tree)
	seen := map[uint32]Node{}
	for i, n := range tree.Nodes {
		if n.Kind != KindBet || n.Street != Draw3 {
			continue
		}
		key := contexts[i]
		if previous, ok := seen[key]; ok {
			if n.Actor != previous.Actor || n.Facing != previous.Facing || n.Wagers != previous.Wagers || n.Commit != previous.Commit || !reflect.DeepEqual(n.Acts, previous.Acts) {
				t.Fatalf("context %d merges different river games: %+v, %+v", key, previous, n)
			}
		}
		seen[key] = n
	}
}

func TestRiverResponseRejectsInvalidRows(t *testing.T) {
	for _, p := range []*RiverPolicy{
		{Version: 2, Abstraction: "rank"},
		{Version: 1, Abstraction: "unknown"},
		{Version: 1, Abstraction: "rank", Rows: []RiverRow{{P: [3]uint8{1}, Visits: 1}}},
		{Version: 1, Abstraction: "rank", Rows: []RiverRow{{P: [3]uint8{255}, Visits: 0}}},
		{Version: 1, Abstraction: "rank", Rows: []RiverRow{{P: [3]uint8{255}, Visits: 1}, {P: [3]uint8{255}, Visits: 1}}},
	} {
		var data bytes.Buffer
		if err := p.Encode(&data); err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeRiverPolicy(data.Bytes()); err == nil {
			t.Fatalf("accepted invalid policy %+v", p)
		}
	}
}

func TestRiverResponseParallelTrainingAndSnapshot(t *testing.T) {
	tree := &Tree{Nodes: []Node{
		{Kind: KindBet, Street: Draw3, Actor: Btn, Acts: []uint8{Pass, Aggr}, Next: [3]int32{1, 2}},
		{Kind: KindFold, Actor: Btn, Commit: [2]int32{50, 100}},
		{Kind: KindFold, Actor: BB, Commit: [2]int32{50, 100}},
	}}
	tr := NewRiverTrainer(tree, passiveModel{}, passiveModel{}, nil, "rank")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := riverWorker{tr: tr, rng: rand.New(rand.NewPCG(91, uint64(i)))}
			v := responseView("2c", "3d", "4h", "6s", "8c")
			w.state.Hands[Btn] = v.Hand
			w.state.Drawn = v.Drawn
			for j := 1; j <= 100; j++ {
				tr.mu.RLock()
				w.walk(0, Btn, Btn, 1, float64(j))
				tr.mu.RUnlock()
			}
		}(i)
	}
	for i := 0; i < 5; i++ {
		_ = tr.Extract(1)
	}
	wg.Wait()
	p := tr.Extract(1)
	if len(p.Rows) != 1 || p.Rows[0].Visits != 400 || p.Rows[0].P[Aggr] < 250 {
		t.Fatalf("concurrent training lost updates: %+v", p.Rows)
	}
}

func TestRiverResponseRejectsCorruptGzipTrailer(t *testing.T) {
	p := RiverPolicy{Version: 1, Abstraction: "rank"}
	var b bytes.Buffer
	if err := p.Encode(&b); err != nil {
		t.Fatal(err)
	}
	raw := b.Bytes()
	raw[len(raw)-8] ^= 1
	if _, err := DecodeRiverPolicy(raw); err == nil {
		t.Fatal("accepted corrupt checksum")
	}
}
