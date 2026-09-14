package cfr

// PolicyHistoryAt returns the immutable public history before node. Invalid
// node IDs return the zero history.
func PolicyHistoryAt(node int32) PolicyHistory {
	if node < 0 || int(node) >= len(policyHistories) {
		return PolicyHistory{}
	}
	return policyHistories[node]
}

// ProjectDrawNode finds the closest real heads-up draw node to a projected
// multi-seat public history. A valid node is essential because empirical
// policy versions 5+ derive their action-history features from the node ID.
func ProjectDrawNode(street, actor int, target PolicyHistory, lastAggressor int) (int32, bool) {
	if street < Draw1 || street > Draw3 || actor < Btn || actor > BB {
		return -1, false
	}
	tree := projectionTree
	best, bestDistance := int32(-1), int(^uint(0)>>1)
	for id := range tree.Nodes {
		n := &tree.Nodes[id]
		if n.Kind != KindDraw || int(n.Street) != street || int(n.Actor) != actor {
			continue
		}
		distance := historyDistance(target, policyHistories[id])
		if projectionLastAggressors[id] != lastAggressor {
			distance += 8
		}
		if distance < bestDistance {
			best, bestDistance = int32(id), distance
		}
	}
	return best, best >= 0
}

func historyDistance(a, b PolicyHistory) int {
	weights := [3]int{16, 1, 3}
	distance := 0
	for street := Predraw; street < Streets; street++ {
		for seat := Btn; seat <= BB; seat++ {
			for action, weight := range weights {
				delta := int(a[street][seat][action]) - int(b[street][seat][action])
				if delta < 0 {
					delta = -delta
				}
				distance += weight * delta
			}
		}
	}
	return distance
}

var projectionTree = BuildTree()
var projectionLastAggressors = drawNodeLastAggressors(projectionTree)

func drawNodeLastAggressors(tree *Tree) []int {
	last := make([]int, len(tree.Nodes))
	seen := make([]bool, len(tree.Nodes))
	var visit func(int32, int)
	visit = func(id int32, aggressor int) {
		if id < 0 || int(id) >= len(tree.Nodes) || seen[id] {
			return
		}
		seen[id], last[id] = true, aggressor
		n := &tree.Nodes[id]
		switch n.Kind {
		case KindDraw:
			visit(n.Next[0], aggressor)
		case KindBet:
			for i, action := range n.Acts {
				nextAggressor := aggressor
				if action == Aggr {
					nextAggressor = int(n.Actor)
				}
				visit(n.Next[i], nextAggressor)
			}
		}
	}
	visit(tree.Root, -1)
	return last
}
