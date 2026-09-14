package cfr

import "testing"

func TestProjectDrawNodePreservesExactHeadsUpHistory(t *testing.T) {
	tree := BuildTree()
	node := tree.Root
	node = childFor(tree, node, Aggr)
	node = childFor(tree, node, Pass)
	for street := Draw1; street <= Draw3; street++ {
		for actor := BB; actor >= Btn; actor-- {
			if tree.Nodes[node].Kind != KindDraw || int(tree.Nodes[node].Street) != street || int(tree.Nodes[node].Actor) != actor {
				t.Fatalf("street %d actor %d path ended at %+v", street, actor, tree.Nodes[node])
			}
			history := PolicyHistoryAt(node)
			got, ok := ProjectDrawNode(street, actor, history, Btn)
			if !ok || got != node {
				t.Fatalf("street %d actor %d projected node = %d, %v; want %d", street, actor, got, ok, node)
			}
			node = tree.Nodes[node].Next[0]
		}
		if street < Draw3 {
			node = childFor(tree, node, Pass)
			node = childFor(tree, node, Pass)
		}
	}
}

func TestProjectDrawNodeUsesPressureAndStableTieBreak(t *testing.T) {
	tree := BuildTree()
	var target PolicyHistory
	target[Predraw][Btn][HistoryAggressive] = 1
	target[Predraw][BB][HistoryCall] = 1
	first, ok := ProjectDrawNode(Draw2, BB, target, Btn)
	if !ok {
		t.Fatal("no draw projection")
	}
	for i := 0; i < 10; i++ {
		got, ok := ProjectDrawNode(Draw2, BB, target, Btn)
		if !ok || got != first {
			t.Fatalf("projection changed: %d/%v then %d/%v", first, true, got, ok)
		}
	}
	n := tree.Nodes[first]
	if n.Kind != KindDraw || n.Street != Draw2 || n.Actor != BB {
		t.Fatalf("invalid projected node %+v", n)
	}
	projected := PolicyHistoryAt(first)
	if projected[Predraw][Btn][HistoryAggressive] != 1 || projected[Predraw][BB][HistoryCall] != 1 {
		t.Fatalf("raise/call pressure lost to cheaper check mismatch: %+v", projected)
	}
}

func TestProjectDrawNodeRejectsInvalidRequest(t *testing.T) {
	if _, ok := ProjectDrawNode(Predraw, Btn, PolicyHistory{}, -1); ok {
		t.Fatal("predraw cannot project to a draw node")
	}
}

func childFor(t *Tree, node int32, action int) int32 {
	n := &t.Nodes[node]
	for i, candidate := range n.Acts {
		if int(candidate) == action {
			return n.Next[i]
		}
	}
	return -1
}
