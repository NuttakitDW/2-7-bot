package cfr

import "testing"

func TestSimulateCountsFallbacksAcrossSeatRotations(t *testing.T) {
	tree := &Tree{Nodes: []Node{
		{Kind: KindBet, Actor: Btn, Acts: []uint8{Pass}, Next: [3]int32{1}},
		{Kind: KindFold, Actor: BB, Commit: [2]int32{50, 100}},
	}}
	r := Simulate(tree, nil, missingModel{}, passiveModel{}, passiveModel{}, 10, 7)
	if r.Decisions != [2]int{10, 10} || r.Fallbacks != [2]int{10, 0} {
		t.Fatalf("decisions=%v fallbacks=%v", r.Decisions, r.Fallbacks)
	}
	if r.Rate != 0 {
		t.Fatalf("identical effective policies scored %v", r.Rate)
	}
}

type missingModel struct{}

func (missingModel) Bet(*View) (int, bool)    { return Pass, false }
func (missingModel) Draw(*View) (uint8, bool) { return 0, false }
