package cfr

import (
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

// Continuations: 7 betting sequences carry predraw into draw1 (limp-check,
// limp-raise-call and the three-bet/cap variants, raise-call and the
// re-raise variants), and 9 carry each draw street into the next.
func TestTreeShape(t *testing.T) {
	tree := BuildTree()
	bet, draw := tree.Counts()
	wantBet := 8 + 7*10 + 7*9*10 + 7*9*9*10
	wantDraw := 2 * (7 + 7*9 + 7*9*9)
	if bet != wantBet || draw != wantDraw {
		t.Fatalf("bet/draw nodes = %d/%d, want %d/%d", bet, draw, wantBet, wantDraw)
	}

	root := &tree.Nodes[tree.Root]
	if root.Actor != Btn || !root.Facing || len(root.Acts) != 3 || root.Wagers != 1 {
		t.Fatalf("root = %+v", *root)
	}
	// Button folds predraw: loses the small blind.
	fold := &tree.Nodes[root.Next[0]]
	if fold.Kind != KindFold || fold.Payoff(Btn, -1) != -SmallBlind || fold.Payoff(BB, -1) != SmallBlind {
		t.Fatalf("fold = %+v", *fold)
	}
	// Limp, then the big blind's option.
	option := &tree.Nodes[root.Next[1]]
	if option.Actor != BB || option.Facing || len(option.Acts) != 2 {
		t.Fatalf("option = %+v", *option)
	}
	// Check behind ends the round: the big blind draws first on draw1.
	bbDraw := &tree.Nodes[option.Next[0]]
	if bbDraw.Kind != KindDraw || bbDraw.Street != Draw1 || bbDraw.Actor != BB {
		t.Fatalf("draw1 = %+v", *bbDraw)
	}
	btnDraw := &tree.Nodes[bbDraw.Next[0]]
	if btnDraw.Kind != KindDraw || btnDraw.Actor != Btn {
		t.Fatalf("button draw1 = %+v", *btnDraw)
	}
	open := &tree.Nodes[btnDraw.Next[0]]
	if open.Kind != KindBet || open.Actor != BB || open.Facing || open.Wagers != 0 || open.Commit != [2]int32{100, 100} {
		t.Fatalf("draw1 betting = %+v", *open)
	}
}

func TestTreeCapAndBigBets(t *testing.T) {
	tree := BuildTree()
	// raise, re-raise, cap: the big blind may only fold or call.
	id := tree.Root
	for i := 0; i < 3; i++ {
		node := &tree.Nodes[id]
		id = node.Next[len(node.Acts)-1]
	}
	capped := &tree.Nodes[id]
	if capped.Wagers != RaiseCap || len(capped.Acts) != 2 || capped.Actor != BB {
		t.Fatalf("capped = %+v", *capped)
	}
	if capped.Commit != [2]int32{400, 300} {
		t.Fatalf("capped commit = %v", capped.Commit)
	}
	// Call, draw, draw, then bet-call on draw1, draw, draw, then a bet on
	// draw2 is a big bet.
	id = capped.Next[1]
	id = tree.Nodes[tree.Nodes[id].Next[0]].Next[0]
	node := &tree.Nodes[id]
	id = node.Next[1] // bet
	id = tree.Nodes[id].Next[1]
	id = tree.Nodes[tree.Nodes[id].Next[0]].Next[0]
	draw2 := &tree.Nodes[id]
	if draw2.Street != Draw2 || draw2.Commit != [2]int32{500, 500} {
		t.Fatalf("draw2 = %+v", *draw2)
	}
	afterBet := &tree.Nodes[draw2.Next[1]]
	if afterBet.Commit != [2]int32{500, 700} || !afterBet.Facing || afterBet.Actor != Btn {
		t.Fatalf("after big bet = %+v", *afterBet)
	}
}

func TestPayoffShowdown(t *testing.T) {
	node := Node{Kind: KindShowdown, Commit: [2]int32{700, 700}}
	if node.Payoff(Btn, Btn) != 700 || node.Payoff(Btn, BB) != -700 || node.Payoff(Btn, -1) != 0 {
		t.Fatal("showdown payoffs")
	}
}

func TestAbstractionBuckets(t *testing.T) {
	abs := BuildAbstraction()
	if abs.NumDrawClasses != 3653 {
		t.Fatalf("draw classes = %d, want 3653: every rank set of one to five ranks bar the thirteen five-of-a-kinds", abs.NumDrawClasses)
	}
	tests := []struct {
		hand        string
		draw, final uint8
		cands       uint8
	}{
		{"7c 5d 4h 3s 2c", 0, 0, 3},
		{"8c 7d 6h 3s 2c", 4, 6, 3},
		{"9c 8d 4h 3s 2c", 7, 9, 3},
		{"Jc 7d 4h 3s 2c", 10, 13, 3},
		{"Kc Qd 8h 3s 2c", 16, 15, 3},
		{"Kc Qd 5h 3s 2c", 13, 15, 3},
		{"7c 7d 4h 3s 2c", 10, 16, 3},
		{"6c 5d 4h 3s 2c", 10, 17, 3},
	}
	for _, test := range tests {
		hand := cards.SortedByRank(cards.MustParse(splitWords(test.hand)...))
		info := abs.Classes[Class(hand)]
		if info.Draw != test.draw || info.Final != test.final {
			t.Errorf("%s: draw/final = %d/%d, want %d/%d", test.hand, info.Draw, info.Final, test.draw, test.final)
		}
		if info.NumCand != test.cands {
			t.Errorf("%s: %d candidates, want %d", test.hand, info.NumCand, test.cands)
		}
		if info.Keep[0] != 0x1F {
			t.Errorf("%s: candidate 0 must be the stand pat, got %05b", test.hand, info.Keep[0])
		}
	}
}

func TestKeepMaskDropsPairCopy(t *testing.T) {
	abs := BuildAbstraction()
	hand := cards.SortedByRank(cards.MustParse("7c", "7d", "4h", "3s", "2c"))
	info := abs.Classes[Class(hand)]
	// Structural keep is 2-3-4-7: one seven, the first copy in sorted order.
	if info.Keep[1] != 0b01111 {
		t.Fatalf("keep = %05b", info.Keep[1])
	}
}

func splitWords(s string) []string {
	var out []string
	word := ""
	for _, c := range s {
		if c == ' ' {
			if word != "" {
				out = append(out, word)
			}
			word = ""
			continue
		}
		word += string(c)
	}
	if word != "" {
		out = append(out, word)
	}
	return out
}
