package onyx

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestRiverPriorSuitExpansionConditionsOnKnownCards(t *testing.T) {
	raw := []byte(`{"river_priors":[{"own_tag":2,"opponent_tag":0,"hands":[{"hand":"2c3d4h5s7c","weight":3},{"hand":"2d3h4s5cKc","weight":1}]}]}`)
	model, err := decodeRiverPriors(raw)
	if err != nil {
		t.Fatal(err)
	}
	var drawn [2]cfr.DrawCounts
	drawn[1][2], drawn[1][3] = 1, 1
	known := cards.NewSet(cards.MustParse("7c", "7d", "7h", "7s"))
	prior := model.prior(0, drawn, known)
	if len(prior) != 1 || prior[0].Weight != 1 || prior[0].Hand[4].Rank != cards.King {
		t.Fatalf("blocked rank survived %+v", prior)
	}
	if cards.NewSet(prior[0].Hand[:])&known != 0 {
		t.Fatal("blocked card survived")
	}
	if len(model.prior(1, drawn, 0)) != 0 {
		t.Fatal("swapped hero and opponent tags")
	}
}

func TestRangeResponseOverridesFittedRaiseWithLosingCallEV(t *testing.T) {
	s, hand := riverTracker(t, 0)
	var err error
	s.priors, err = decodeRiverPriors([]byte(`{"river_priors":[{"own_tag":0,"opponent_tag":0,"hands":[{"hand":"2c3d4h5s7c","weight":1}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	s.model = &cfr.Empirical{Version: 1}
	s.model.Betting[3] = []cfr.PolicyNode{{Feature: -1, Prob: [6]float64{0, 0, 1}}}
	for id, n := range s.tree.Nodes {
		if n.Kind == cfr.KindBet && n.Street == 3 && n.Actor == 0 && n.Facing && n.Wagers == 1 {
			s.node = int32(id)
			break
		}
	}
	call := uint64(200)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call, Raise: &wire.Range{MinTo: 400, MaxTo: 400}}
	if a, ok := s.mirrorFrom(0, hand, d, cfr.Draw1); !ok || a.Kind != wire.ActionFold {
		t.Fatalf("ignored posterior EV %+v %v", a, ok)
	}
}

func TestRiverPriorRejectsBrokenInput(t *testing.T) {
	for _, raw := range []string{`{}`, `{"river_priors":[{"own_tag":5}]}`, `{"river_priors":[{"own_tag":0,"opponent_tag":0,"hands":[{"hand":"2c2c4h5s7c","weight":1}]}]}`} {
		if _, err := decodeRiverPriors([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func BenchmarkRangeResponse(b *testing.B) {
	s, hand := riverTracker(b, 0)
	var err error
	s.priors, err = decodeRiverPriors(opponentPolicyData)
	if err != nil {
		b.Skip("requires generated river-prior artifact")
	}
	v := s.view(&s.tree.Nodes[s.node])
	copy(v.Hand[:], hand)
	v.Drawn[0][2], v.Drawn[0][3], v.Drawn[1][2], v.Drawn[1][3] = 1, 1, 1, 1
	b.ResetTimer()
	for b.Loop() {
		if _, ok := s.rangeResponse(&v); !ok {
			b.Fatal("missing response")
		}
	}
}
