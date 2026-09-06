package onyx

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/deuce"
)

// These are offline equilibria for finite estimated river ranges. Runtime
// interpolation is an approximation; the exported convergence metric does not
// certify the full game or a best response to a particular opponent.
type equilibriumRow struct {
	Seat    int        `json:"seat"`
	Hand    string     `json:"hand"`
	History string     `json:"history"`
	P       [3]float64 `json:"p"`
}
type equilibriumGame struct {
	OOPTag         int              `json:"oop_tag"`
	IPTag          int              `json:"ip_tag"`
	Pot            int32            `json:"pot"`
	Exploitability float64          `json:"exploitability_chips"`
	Strategies     []equilibriumRow `json:"strategies"`
	rows           map[string][]equilibriumPoint
}
type equilibriumPoint struct {
	value deuce.Value
	p     [3]float64
	count int
}
type riverContext struct {
	History string
	Pot     int32
	Seat    int
}
type riverEquilibrium struct {
	games    [4][4][]equilibriumGame
	contexts []riverContext
}

func riverContexts(tree *cfr.Tree) []riverContext {
	contexts := make([]riverContext, len(tree.Nodes))
	var visit func(int32, string, int32)
	visit = func(id int32, history string, pot int32) {
		n := &tree.Nodes[id]
		if n.Kind != cfr.KindBet {
			return
		}
		contexts[id] = riverContext{history, pot, int(n.Actor)}
		for i, a := range n.Acts {
			code := "f"
			if a == cfr.Aggr {
				code = "r"
			} else if a == cfr.Pass {
				code = "x"
				if n.Facing {
					code = "c"
				}
			}
			visit(n.Next[i], history+code, pot)
		}
	}
	for id, n := range tree.Nodes {
		if n.Kind == cfr.KindBet && n.Street == cfr.Draw3 && n.Wagers == 0 && int(n.Actor) == cfr.BB {
			visit(int32(id), "", n.Commit[0]+n.Commit[1])
		}
	}
	return contexts
}

func decodeRiverEquilibrium(raw []byte, tree *cfr.Tree) (*riverEquilibrium, error) {
	raw, err := modelJSON(raw)
	if err != nil {
		return nil, err
	}
	var data struct {
		Games []equilibriumGame `json:"river_equilibrium"`
	}
	if err = json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	if len(data.Games) == 0 || len(data.Games) > 128 {
		return nil, fmt.Errorf("invalid equilibrium game count")
	}
	model := &riverEquilibrium{contexts: riverContexts(tree)}
	legalHistories := map[string]int{}
	for _, context := range model.contexts {
		if context.Pot > 0 {
			legalHistories[context.History] = context.Seat
		}
	}
	for _, game := range data.Games {
		if game.OOPTag < 0 || game.OOPTag > 3 || game.IPTag < 0 || game.IPTag > 3 || game.Pot <= 0 || game.Pot > 1000 || math.IsNaN(game.Exploitability) || math.IsInf(game.Exploitability, 0) || game.Exploitability < 0 || game.Exploitability > .00101 || len(game.Strategies) == 0 || len(game.Strategies) > 10000 {
			return nil, fmt.Errorf("invalid or unconverged equilibrium game")
		}
		for _, old := range model.games[game.OOPTag][game.IPTag] {
			if old.Pot == game.Pot*50 {
				return nil, fmt.Errorf("duplicate equilibrium context")
			}
		}
		game.rows = map[string][]equilibriumPoint{}
		for _, row := range game.Strategies {
			seat, ok := legalHistories[row.History]
			if !ok || seat != row.Seat || len(row.Hand) != 10 {
				return nil, fmt.Errorf("invalid equilibrium information set")
			}
			hand := make([]cards.Card, 5)
			for i := range hand {
				hand[i], err = cards.ParseCard(row.Hand[2*i : 2*i+2])
				if err != nil {
					return nil, err
				}
			}
			if cards.NewSet(hand).Len() != 5 {
				return nil, fmt.Errorf("duplicate equilibrium card")
			}
			sum := 0.0
			for _, p := range row.P {
				if math.IsNaN(p) || math.IsInf(p, 0) || p < 0 || p > 1 {
					return nil, fmt.Errorf("invalid equilibrium probability")
				}
				sum += p
			}
			if math.Abs(sum-1) > 1e-6 {
				return nil, fmt.Errorf("unnormalized equilibrium strategy")
			}
			point := equilibriumPoint{deuce.Eval(hand), row.P, 1}
			game.rows[row.History] = append(game.rows[row.History], point)
		}
		for history, points := range game.rows {
			sort.Slice(points, func(i, j int) bool { return points[i].value < points[j].value })
			merged := points[:0]
			for _, point := range points {
				if len(merged) > 0 && merged[len(merged)-1].value == point.value {
					last := &merged[len(merged)-1]
					for a := range last.p {
						last.p[a] += point.p[a]
					}
					last.count++
				} else {
					merged = append(merged, point)
				}
			}
			for i := range merged {
				for a := range merged[i].p {
					merged[i].p[a] /= float64(merged[i].count)
				}
			}
			game.rows[history] = merged
		}
		game.Pot *= 50 // Solver small/big bets 2/4 correspond to arena chips 100/200.
		game.Strategies = nil
		model.games[game.OOPTag][game.IPTag] = append(model.games[game.OOPTag][game.IPTag], game)
	}
	return model, nil
}

func equilibriumDrawTag(d cfr.DrawCounts) int {
	if d[2] < 0 || d[3] < 0 {
		return -1
	}
	if d[3] == 0 {
		if d[2] == 0 {
			return 0
		}
		return 1
	}
	if d[3] == 1 {
		return 2
	}
	return 3
}

func (m *riverEquilibrium) action(v *cfr.View, u float64) (int, bool) {
	if v.Street != cfr.Draw3 || v.Node < 0 || int(v.Node) >= len(m.contexts) || u < 0 || u >= 1 || math.IsNaN(u) {
		return 0, false
	}
	context := m.contexts[v.Node]
	if context.Pot == 0 || context.Seat != v.Seat {
		return 0, false
	}
	oop, ip := equilibriumDrawTag(v.Drawn[cfr.BB]), equilibriumDrawTag(v.Drawn[cfr.Btn])
	if oop < 0 || ip < 0 {
		return 0, false
	}
	games := m.games[oop][ip]
	if len(games) == 0 {
		return 0, false
	}
	best := 0
	for i := 1; i < len(games); i++ {
		if math.Abs(float64(games[i].Pot-context.Pot)) < math.Abs(float64(games[best].Pot-context.Pot)) {
			best = i
		}
	}
	points := games[best].rows[context.History]
	if len(points) == 0 {
		return 0, false
	}
	value := deuce.Eval(v.Hand[:])
	i := sort.Search(len(points), func(i int) bool { return points[i].value >= value })
	var p [3]float64
	if i == 0 {
		p = points[0].p
	} else if i == len(points) {
		p = points[i-1].p
	} else {
		fraction := float64(value-points[i-1].value) / float64(points[i].value-points[i-1].value)
		for a := range p {
			p[a] = (1-fraction)*points[i-1].p[a] + fraction*points[i].p[a]
		}
	}
	if !v.Facing {
		p[cfr.Fold] = 0
	}
	if !v.CanRaise {
		p[cfr.Aggr] = 0
	}
	total := p[0] + p[1] + p[2]
	if total <= 0 {
		return 0, false
	}
	at := u * total
	for a, prob := range p {
		at -= prob
		if at < 0 {
			return a, true
		}
	}
	return cfr.Pass, true
}
