package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nuttakit/2-7-bot/internal/arena"
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
)

type policySample struct {
	Match  int                             `json:"match"`
	Street int                             `json:"street"`
	Draw   bool                            `json:"draw"`
	X      [cfr.PolicyFeatureCount]float64 `json:"x"`
	Y      int                             `json:"y"`
	// Keep labels the actual retained positions in the sorted pre-draw
	// hand. It is an offline target, never an input feature.
	Keep *uint8 `json:"keep_mask,omitempty"`
}

func policySamples(h arena.HandDetail, target int, tree *cfr.Tree) ([]policySample, error) {
	if target < 0 || target > 1 || len(h.Hand.Roles) != 2 {
		return nil, fmt.Errorf("invalid policy seats")
	}
	seats := [2]int{-1, -1}
	for p, r := range h.Hand.Roles {
		switch r {
		case "smallblind":
			seats[p] = cfr.Btn
		case "bigblind":
			seats[p] = cfr.BB
		}
	}
	if seats[0] < 0 || seats[1] < 0 || seats[0] == seats[1] {
		return nil, fmt.Errorf("invalid roles")
	}
	var hand []cards.Card
	var drawn [2]cfr.DrawCounts
	for p := range drawn {
		for s := range drawn[p] {
			drawn[p][s] = -1
		}
	}
	node, lastAggr := tree.Root, -1
	var out []policySample
	for _, e := range h.Events {
		if e.Player == nil || *e.Player < 0 || *e.Player > 1 {
			continue
		}
		p := *e.Player
		if p == target && e.Kind == "initial-cards" {
			if e.Cards == nil {
				return nil, fmt.Errorf("missing initial cards")
			}
			var err error
			hand, err = packed(*e.Cards)
			if err != nil {
				return nil, err
			}
		}
		if e.Kind != "action" && e.Kind != "replacement" {
			continue
		}
		n := &tree.Nodes[node]
		if int(n.Actor) != seats[p] || e.Street == nil || *e.Street != [4]string{"predraw", "draw1", "draw2", "draw3"}[n.Street] {
			return nil, fmt.Errorf("event disagrees with public tree at %d", e.ID)
		}
		v := cfr.View{Seat: seats[p], Node: node, Street: int(n.Street), Facing: n.Facing, Wagers: int(n.Wagers), Drawn: drawn, LastAggr: lastAggr}
		if n.Kind == cfr.KindBet {
			v.Pot = n.Commit[0] + n.Commit[1]
			v.ToCall = max(0, n.Commit[1-v.Seat]-n.Commit[v.Seat])
			v.CanRaise = n.Acts[len(n.Acts)-1] == cfr.Aggr
		}
		if p == target {
			if len(hand) != 5 || cards.NewSet(hand).Len() != 5 {
				return nil, fmt.Errorf("invalid private hand")
			}
			copy(v.Hand[:], cards.SortedByRank(hand))
		}
		y := -1
		var keep *uint8
		if e.Kind == "action" {
			if n.Kind != cfr.KindBet || e.Action == nil {
				return nil, fmt.Errorf("invalid betting event")
			}
			switch *e.Action {
			case "fold":
				y = cfr.Fold
			case "call", "check":
				y = cfr.Pass
			case "raise", "bet":
				y = cfr.Aggr
			}
			found := false
			for i, a := range n.Acts {
				if int(a) == y {
					node = n.Next[i]
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("illegal recorded action")
			}
			if y == cfr.Aggr {
				lastAggr = seats[p]
			}
		} else {
			if n.Kind != cfr.KindDraw || e.Cards == nil || e.Detail == nil {
				return nil, fmt.Errorf("invalid draw event")
			}
			in, err := packed(*e.Cards)
			if err != nil {
				return nil, err
			}
			discard, err := packed(*e.Detail)
			if err != nil {
				return nil, err
			}
			if len(in) != len(discard) {
				return nil, fmt.Errorf("unbalanced replacement")
			}
			y = len(in)
			if p == target {
				mask := uint8(31)
				for _, card := range discard {
					found := false
					for i, held := range v.Hand {
						if held == card && mask&(1<<i) != 0 {
							mask &^= 1 << i
							found = true
							break
						}
					}
					if !found {
						return nil, fmt.Errorf("discard not in private hand")
					}
				}
				keep = &mask
				hand = cards.With(cards.Without(hand, discard), in)
			}
			drawn[seats[p]][n.Street] = int8(y)
			node = n.Next[0]
		}
		if p == target {
			out = append(out, policySample{Street: v.Street, Draw: e.Kind == "replacement", X: cfr.PolicyFeatures(&v), Y: y, Keep: keep})
		}
	}
	return out, nil
}

func generatePolicy(metadata, dir, target, out string) error {
	raw, err := os.ReadFile(metadata)
	if err != nil {
		return err
	}
	var matches []arena.MatchSummary
	if err = json.Unmarshal(raw, &matches); err != nil {
		return err
	}
	seats := map[int]int{}
	for _, m := range matches {
		if len(m.Players) == 2 {
			for p, name := range m.Players {
				if name == target {
					seats[m.ID] = p
				}
			}
		}
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return err
	}
	tree := cfr.BuildTree()
	var all []policySample
	for _, path := range files {
		id, err := strconv.Atoi(strings.Split(filepath.Base(path), "-")[0])
		if err != nil {
			return err
		}
		seat, ok := seats[id]
		if !ok {
			return fmt.Errorf("missing target in match %d", id)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var h arena.HandDetail
		if err = json.Unmarshal(raw, &h); err != nil {
			return err
		}
		samples, err := policySamples(h, seat, tree)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		for i := range samples {
			samples[i].Match = id
		}
		all = append(all, samples...)
	}
	if len(all) == 0 {
		return fmt.Errorf("no policy samples")
	}
	raw, err = json.Marshal(all)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "exported %d policy decisions\n", len(all))
	return os.WriteFile(out, raw, 0644)
}
