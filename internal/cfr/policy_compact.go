package cfr

// compactTree is a fitted tree flattened for prediction: 24 bytes a node
// instead of 80, with leaf distributions stored once, already normalised.
// Decision trees walk one root-to-leaf path per query, so the cost is
// almost entirely cache misses, and a third of the footprint is a third
// of the misses.
type compactTree struct {
	nodes  []compactNode
	leaves [][6]float64
}

type compactNode struct {
	feature   int16 // -1 at a leaf; then left indexes leaves
	_         int16
	left      int32
	right     int32
	threshold float64
}

func compileTree(nodes []PolicyNode) compactTree {
	t := compactTree{nodes: make([]compactNode, len(nodes))}
	for i, n := range nodes {
		if n.Feature < 0 {
			total := 0.0
			for _, v := range n.Prob {
				total += v
			}
			p := n.Prob
			for k := range p {
				p[k] /= total
			}
			t.nodes[i] = compactNode{feature: -1, left: int32(len(t.leaves))}
			t.leaves = append(t.leaves, p)
			continue
		}
		t.nodes[i] = compactNode{feature: int16(n.Feature), left: int32(n.Left), right: int32(n.Right), threshold: n.Threshold}
	}
	return t
}

func compileForest(forest [][]PolicyNode) []compactTree {
	out := make([]compactTree, len(forest))
	for i, nodes := range forest {
		out[i] = compileTree(nodes)
	}
	return out
}

func (t *compactTree) predict(x *[PolicyFeatureCount]float64) *[6]float64 {
	i := int32(0)
	for {
		n := &t.nodes[i]
		if n.feature < 0 {
			return &t.leaves[n.left]
		}
		if x[n.feature] <= n.threshold {
			i = n.left
		} else {
			i = n.right
		}
	}
}

func predictCompact(forest []compactTree, x *[PolicyFeatureCount]float64) [6]float64 {
	var out [6]float64
	for i := range forest {
		p := forest[i].predict(x)
		for k := range out {
			out[k] += p[k]
		}
	}
	inv := 1 / float64(len(forest))
	for k := range out {
		out[k] *= inv
	}
	return out
}
