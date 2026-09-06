package onyx

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/table"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

func TestModeledOpeningIsExplicitAndScoped(t *testing.T) {
	old := modelSelection
	t.Cleanup(func() { modelSelection = old })
	s, hand := riverTracker(t, 1)
	s.reset()
	s.model = &cfr.Empirical{Version: 1}
	s.model.Betting[0] = []cfr.PolicyNode{{Feature: -1, Prob: [6]float64{0, 0, 1}}}
	b := &Bot{Table: table.New(), bayes: s}
	b.Table.Hand.Seat, b.Table.Hand.Cards = cfr.Btn, hand
	call := uint64(50)
	d := wire.Decision{Kind: wire.DecisionWager, Call: &call, Fold: true, Raise: &wire.Range{MinTo: 200, MaxTo: 200}}
	modelSelection = "draw-model"
	if _, ok := b.MirrorPredraw(d); ok {
		t.Fatal("changed baseline opening")
	}
	modelSelection = "all-model"
	if a, ok := b.MirrorPredraw(d); !ok || a.Kind != wire.ActionRaise {
		t.Fatalf("missing modeled opening %+v %v", a, ok)
	}
	modelSelection = "all-bets"
	if a, ok := b.MirrorPredraw(d); !ok || a.Kind != wire.ActionRaise {
		t.Fatalf("missing betting-only opening %+v %v", a, ok)
	}
	for street := range s.model.Drawing {
		s.model.Drawing[street] = []cfr.PolicyNode{{Feature: -1, Prob: [6]float64{1}}}
	}
	for id, n := range s.tree.Nodes {
		if n.Kind == cfr.KindDraw && n.Actor == cfr.Btn {
			s.node = int32(id)
			break
		}
	}
	if _, ok := b.MirrorDraw(wire.Decision{Kind: wire.DecisionDraw}); ok {
		t.Fatal("betting-only profile changed drawing")
	}
	b.Table.Hand.Street = table.Draw1
	if _, ok := b.MirrorPredraw(d); ok {
		t.Fatal("answered postdraw with opening hook")
	}
}

func TestRiverRangeInterceptionPreservesOtherDecisions(t *testing.T) {
	s, hand := riverTracker(t, 1)
	freeNode := s.node
	bluff := uint32(deuce.Eval(cards.MustParse("2c", "3d", "4h", "5s", "6c")))
	s.riverRange = &riverRangeModel{Forest: [][]riverRangeNode{{{Feature: -1, rangeValues: makeRange([]weightedValue{{bluff, 1}})}}}}
	s.model = &cfr.Empirical{Version: 1}
	for street := 1; street <= 3; street++ {
		s.model.Betting[street] = []cfr.PolicyNode{{Feature: -1, Prob: [6]float64{1, 0, 0}}}
	}
	call := uint64(200)
	d := wire.Decision{Kind: wire.DecisionWager, Fold: true, Call: &call, Raise: &wire.Range{MinTo: 400, MaxTo: 400}}
	for street := 2; street <= 3; street++ {
		found := false
		for id, n := range s.tree.Nodes {
			if n.Kind != cfr.KindBet || int(n.Street) != street || n.Actor != 1 || !n.Facing || n.Wagers != 1 {
				continue
			}
			s.node = int32(id)
			want := wire.ActionFold
			if street == 3 {
				want = wire.ActionCall
			}
			if a, ok := s.mirrorFrom(1, hand, d, cfr.Draw1); !ok || a.Kind != want {
				t.Fatalf("street%d: %+v %v", street, a, ok)
			}
			if street == 3 {
				s.model.Betting[3][0].Prob = [6]float64{0, 0, 1}
				if a, ok := s.mirrorFrom(1, hand, d, cfr.Draw1); !ok || a.Kind != wire.ActionRaise {
					t.Fatalf("removed raise: %+v %v", a, ok)
				}
			}
			found = true
			break
		}
		if !found {
			t.Fatal("missing test node")
		}
	}
	s.node = freeNode
	s.model.Betting[3][0].Prob = [6]float64{0, 1, 0}
	if a, ok := s.mirrorFrom(1, hand, wire.Decision{Kind: wire.DecisionWager, Check: true}, cfr.Draw1); !ok || a.Kind != wire.ActionCheck {
		t.Fatalf("changed free check: %+v %v", a, ok)
	}
}

func TestMirrorRiverUsesLegalContext(t *testing.T) {
	s, hand := riverTracker(t, 1)
	s.model = &cfr.Empirical{Version: 1}
	s.model.Betting[3] = []cfr.PolicyNode{{Feature: -1, Prob: [6]float64{0, 0, 1}}}
	d := wire.Decision{Kind: wire.DecisionWager, Check: true, Bet: &wire.Range{MinTo: 200, MaxTo: 200}}
	if a, ok := s.mirrorFrom(1, hand, d, cfr.Draw1); !ok || a.Kind != wire.ActionBet || a.To != 200 {
		t.Fatalf("got %+v %v", a, ok)
	}
	if _, ok := s.mirrorFrom(0, hand, d, cfr.Draw1); ok {
		t.Fatal("answered other seat")
	}
	if _, ok := s.mirrorFrom(1, hand, wire.Decision{Kind: wire.DecisionDraw}, cfr.Draw1); ok {
		t.Fatal("answered a draw")
	}
	s.node = -1
	if _, ok := s.mirrorFrom(1, hand, d, cfr.Draw1); ok {
		t.Fatal("answered lost tracker")
	}
}

func TestMirrorPostdrawUsesEarlierBettingAndCap(t *testing.T) {
	s, hand := riverTracker(t, 1)
	s.model = &cfr.Empirical{Version: 1}
	for street := 1; street <= 3; street++ {
		s.model.Betting[street] = []cfr.PolicyNode{{Feature: -1, Prob: [6]float64{0, .1, .9}}}
	}
	call := uint64(200)
	for id, n := range s.tree.Nodes {
		if n.Kind == cfr.KindBet && n.Street == cfr.Draw2 && n.Actor == 1 && n.Facing && n.Wagers == 4 {
			s.node = int32(id)
			a, ok := s.mirrorFrom(1, hand, wire.Decision{Kind: wire.DecisionWager, Call: &call, Fold: true}, cfr.Draw1)
			if !ok || a.Kind != wire.ActionCall {
				t.Fatalf("capped action %+v %v", a, ok)
			}
			if _, ok := s.mirrorFrom(1, hand, wire.Decision{Kind: wire.DecisionWager, Call: &call}, cfr.Draw3); ok {
				t.Fatal("river-only answered earlier street")
			}
			return
		}
	}
	t.Fatal("no capped node")
}

func TestMirrorDrawUsesOurSortedCardsAndLegalPhase(t *testing.T) {
	s, hand := riverTracker(t, 1)
	s.model = &cfr.Empirical{Version: 1}
	for i := range s.model.Drawing {
		s.model.Drawing[i] = []cfr.PolicyNode{{Feature: -1, Prob: [6]float64{0, 1}}}
	}
	for id, n := range s.tree.Nodes {
		if n.Kind != cfr.KindDraw || n.Actor != 1 || n.Street != cfr.Draw2 {
			continue
		}
		s.node = int32(id)
		d := wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5}
		a, ok := s.mirrorDraw(1, hand, d)
		if !ok || len(a.Cards) != 1 || a.Cards[0] != cards.MustParse("9c")[0] {
			t.Fatalf("wrong private discard %+v %v", a, ok)
		}
		if _, ok := s.mirrorDraw(0, hand, d); ok {
			t.Fatal("answered opponent draw")
		}
		if _, ok := s.mirrorDraw(1, hand, wire.Decision{Kind: wire.DecisionWager}); ok {
			t.Fatal("answered betting")
		}
		s.model.Drawing[1][0].Prob = [6]float64{1}
		if a, ok := s.mirrorDraw(1, hand, d); !ok || len(a.Cards) != 0 {
			t.Fatalf("missed pat %+v %v", a, ok)
		}
		s.node = -1
		if _, ok := s.mirrorDraw(1, hand, d); ok {
			t.Fatal("answered lost tracker")
		}
		return
	}
	t.Fatal("missing draw node")
}
