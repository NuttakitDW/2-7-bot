package onyx

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
)

type riverPriorEntry struct {
	Hand     string  `json:"hand"`
	Weight   float64 `json:"weight"`
	variants [][5]cards.Card
}
type riverPriorGroup struct {
	OwnTag      int               `json:"own_tag"`
	OpponentTag int               `json:"opponent_tag"`
	Hands       []riverPriorEntry `json:"hands"`
}
type riverPriors struct{ groups [4][4][]riverPriorEntry }

func decodeRiverPriors(raw []byte) (*riverPriors, error) {
	raw, err := modelJSON(raw)
	if err != nil {
		return nil, err
	}
	var data struct {
		Groups []riverPriorGroup `json:"river_priors"`
	}
	if err = json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	if len(data.Groups) == 0 || len(data.Groups) > 16 {
		return nil, fmt.Errorf("invalid river prior group count")
	}
	model := &riverPriors{}
	suits := [4]cards.Suit{cards.Clubs, cards.Diamonds, cards.Hearts, cards.Spades}
	for _, group := range data.Groups {
		if group.OwnTag < 0 || group.OwnTag > 3 || group.OpponentTag < 0 || group.OpponentTag > 3 || len(group.Hands) == 0 || len(group.Hands) > 128 {
			return nil, fmt.Errorf("invalid river prior group")
		}
		if model.groups[group.OwnTag][group.OpponentTag] != nil {
			return nil, fmt.Errorf("duplicate river prior group")
		}
		for i := range group.Hands {
			entry := &group.Hands[i]
			if len(entry.Hand) != 10 || entry.Weight <= 0 || math.IsNaN(entry.Weight) || math.IsInf(entry.Weight, 0) {
				return nil, fmt.Errorf("invalid river prior hand")
			}
			var hand [5]cards.Card
			for k := range hand {
				hand[k], err = cards.ParseCard(entry.Hand[2*k : 2*k+2])
				if err != nil {
					return nil, err
				}
			}
			if cards.NewSet(hand[:]).Len() != 5 {
				return nil, fmt.Errorf("duplicate river prior card")
			}
			for a := range suits {
				for b := range suits {
					for c := range suits {
						for d := range suits {
							if a == b || a == c || a == d || b == c || b == d || c == d {
								continue
							}
							permutation := [4]int{a, b, c, d}
							variant := hand
							for k, card := range hand {
								for old, suit := range suits {
									if card.Suit == suit {
										variant[k].Suit = suits[permutation[old]]
										break
									}
								}
							}
							copy(variant[:], cards.SortedByRank(variant[:]))
							entry.variants = append(entry.variants, variant)
						}
					}
				}
			}
		}
		model.groups[group.OwnTag][group.OpponentTag] = group.Hands
	}
	return model, nil
}

// Rank-equivalent suit permutations remove arbitrary training-example suits.
// Collapse compatible variants after card removal: the fitted policy features
// depend on ranks and flushness, which all permutations preserve.
func (m *riverPriors) prior(hero int, drawn [2]cfr.DrawCounts, known cards.Set) []cfr.BeliefHand {
	if hero < 0 || hero > 1 {
		return nil
	}
	own, other := equilibriumDrawTag(drawn[1-hero]), equilibriumDrawTag(drawn[hero])
	if own < 0 || other < 0 {
		return nil
	}
	entries := m.groups[own][other]
	out := make([]cfr.BeliefHand, 0, len(entries))
	total := 0.0
	for _, entry := range entries {
		compatible := 0
		var hand [5]cards.Card
		for _, variant := range entry.variants {
			if cards.NewSet(variant[:])&known == 0 {
				compatible++
				hand = variant
			}
		}
		if compatible == 0 {
			continue
		}
		weight := entry.Weight * float64(compatible) / float64(len(entry.variants))
		out = append(out, cfr.BeliefHand{Hand: hand, Weight: weight})
		total += weight
	}
	if total <= 0 {
		return nil
	}
	for i := range out {
		out[i].Weight /= total
	}
	return out
}

func (s *riverSolver) rangeResponse(v *cfr.View) (int, bool) {
	if s.priors == nil || v.Street != cfr.Draw3 {
		return 0, false
	}
	known := s.known | cards.NewSet(v.Hand[:])
	prior := s.priors.prior(v.Seat, v.Drawn, known)
	posterior := cfr.ConditionRiverBelief(s.model, prior, s.history, known)
	action, _, ok := cfr.BestRiverAction(s.tree, s.node, *v, posterior, s.model)
	return action, ok
}
