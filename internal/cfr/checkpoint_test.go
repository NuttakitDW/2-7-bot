package cfr

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/deuce"
)

func TestStateAndBlueprintRoundTrip(t *testing.T) {
	tree := BuildTree()
	abs := BuildAbstraction()
	layout := NewLayout(tree, abs)
	eval := deuce.NewTable()
	tr := NewTrainer(tree, abs, layout, eval)
	tr.Run(200, 2, 3)

	path := filepath.Join(t.TempDir(), "state")
	if err := tr.SaveState(path); err != nil {
		t.Fatal(err)
	}
	back := NewTrainer(tree, abs, layout, eval)
	if err := back.LoadState(path); err != nil {
		t.Fatal(err)
	}
	if back.Iterations() != tr.Iterations() {
		t.Fatalf("iterations = %d, want %d", back.Iterations(), tr.Iterations())
	}
	for i := range tr.BetStrat {
		if tr.BetStrat[i] != back.BetStrat[i] || tr.BetRegret[i] != back.BetRegret[i] || tr.BetVisits[i] != back.BetVisits[i] {
			t.Fatalf("bet tables differ at %d", i)
		}
	}
	for i := range tr.DrawStrat {
		if tr.DrawStrat[i] != back.DrawStrat[i] || tr.DrawVisits[i] != back.DrawVisits[i] {
			t.Fatalf("draw tables differ at %d", i)
		}
	}

	bp := tr.Extract(1)
	var buf bytes.Buffer
	if err := bp.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(buf.Bytes(), layout)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.Bet, bp.Bet) || !bytes.Equal(decoded.Draw, bp.Draw) {
		t.Fatal("blueprint did not round-trip")
	}
	// Every trained set sums to 255.
	trained := 0
	for i := range tree.Nodes {
		node := &tree.Nodes[i]
		if node.Kind != KindBet {
			continue
		}
		n := int64(len(node.Acts))
		sets := int64(BetContexts(int(node.Street))) * int64(Buckets(int(node.Street)))
		for set := int64(0); set < sets; set++ {
			sum := 0
			for _, p := range bp.Bet[node.Offset+set*n : node.Offset+(set+1)*n] {
				sum += int(p)
			}
			if sum != 0 && sum != 255 {
				t.Fatalf("set sums to %d", sum)
			}
			if sum == 255 {
				trained++
			}
		}
	}
	if trained == 0 {
		t.Fatal("nothing trained")
	}
}
