package cfr

// Histories contain only actions strictly before their public tree node.
// Counts are immutable, shared across model calls, and independent of cards.
var policyHistories = func() [][Streets][2][3]uint8 {
	tree := BuildTree()
	histories := make([][Streets][2][3]uint8, len(tree.Nodes))
	var visit func(int32)
	visit = func(id int32) {
		n := &tree.Nodes[id]
		switch n.Kind {
		case KindDraw:
			histories[n.Next[0]] = histories[id]
			visit(n.Next[0])
		case KindBet:
			for i, action := range n.Acts {
				child := n.Next[i]
				histories[child] = histories[id]
				if action != Fold {
					kind := 0 // Raise or bet.
					if action == Pass {
						kind = 1 // Check.
						if n.Facing {
							kind = 2 // Call.
						}
					}
					histories[child][n.Street][n.Actor][kind]++
				}
				visit(child)
			}
		}
	}
	visit(tree.Root)
	return histories
}()
