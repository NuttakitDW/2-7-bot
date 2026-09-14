// Package garnet learns six-seat predraw entry and action policies over a
// frozen Spinel-draw/Onyx-betting continuation.
package garnet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/bits"
	"sort"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/handclass"
)

const RankingVersion = 1

type Estimate struct {
	Mean  float64 `json:"mean"`
	Count uint64  `json:"count"`
	SE    float64 `json:"se"`
}

type ClassEV struct {
	ID     uint16   `json:"id"`
	Weight int      `json:"weight"`
	Call   Estimate `json:"call"`
	Raise  Estimate `json:"raise"`
	BestEV float64  `json:"best_ev"`
}

// Ranking records position-averaged reference continuation EV in net chips
// relative to folding. Call and raise are estimated independently; BestEV is
// max of their means, never the mean of per-sample maxima.
type Ranking struct {
	Version             int          `json:"version"`
	Seed                uint64       `json:"seed"`
	SamplesPerPosition  int          `json:"samples_per_position"`
	ReferencePolicyHash string       `json:"reference_policy_hash"`
	ReferenceCodeHash   string       `json:"reference_code_hash"`
	EngineHash          string       `json:"engine_hash"`
	ContextMix          string       `json:"context_mix"`
	PositionAveraged    bool         `json:"position_averaged"`
	Stats               RankingStats `json:"stats"`
	Classes             []ClassEV    `json:"classes"`
}

type RankingStats struct {
	PairedSamples [Positions][Contexts]uint64 `json:"paired_samples"`
	DecisionSkips [Positions][Contexts]uint64 `json:"decision_skips"`
}

func (r *Ranking) Validate() error {
	if r == nil || r.Version != RankingVersion {
		return fmt.Errorf("garnet ranking: unsupported version")
	}
	if len(r.ReferencePolicyHash) != 64 {
		return fmt.Errorf("garnet ranking: invalid reference policy hash")
	}
	if len(r.ReferenceCodeHash) != 64 || r.SamplesPerPosition <= 0 {
		return fmt.Errorf("garnet ranking: invalid reference code hash/sample quota")
	}
	if r.EngineHash == "" || r.ContextMix == "" {
		return fmt.Errorf("garnet ranking: missing engine/context metadata")
	}
	if !r.PositionAveraged {
		return fmt.Errorf("garnet ranking: expected position-averaged EV metadata")
	}
	seen := make([]bool, handclass.Num)
	total := 0
	for i, class := range r.Classes {
		id := handclass.ID(class.ID)
		if id >= handclass.Num || seen[id] || handclass.Weight(id) == 0 {
			return fmt.Errorf("garnet ranking: invalid class %d at row %d", class.ID, i)
		}
		seen[id] = true
		if class.Weight != handclass.Weight(id) {
			return fmt.Errorf("garnet ranking: class %d weight %d, want %d", id, class.Weight, handclass.Weight(id))
		}
		if err := validateEstimate(class.Call); err != nil {
			return fmt.Errorf("garnet ranking: class %d call: %w", id, err)
		}
		if err := validateEstimate(class.Raise); err != nil {
			return fmt.Errorf("garnet ranking: class %d raise: %w", id, err)
		}
		if !finite(class.BestEV) || math.Abs(class.BestEV-max(class.Call.Mean, class.Raise.Mean)) > 1e-9 {
			return fmt.Errorf("garnet ranking: class %d inconsistent best EV", id)
		}
		total += class.Weight
	}
	if total != cards.Deals {
		return fmt.Errorf("garnet ranking: physical weight %d, want %d", total, cards.Deals)
	}
	return nil
}

func validateEstimate(e Estimate) error {
	if e.Count == 0 || !finite(e.Mean) || !finite(e.SE) || e.SE < 0 {
		return fmt.Errorf("invalid estimate")
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func EncodeRanking(r *Ranking) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(r, "", "  ")
}

func DecodeRanking(raw []byte) (*Ranking, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var ranking Ranking
	if err := decoder.Decode(&ranking); err != nil {
		return nil, fmt.Errorf("garnet ranking: decode: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("garnet ranking: trailing data")
	}
	if err := ranking.Validate(); err != nil {
		return nil, err
	}
	return &ranking, nil
}

type Position uint8

const (
	Button Position = iota
	SmallBlind
	BigBlind
	UnderTheGun
	Hijack
	Cutoff
	Positions
)

type Context uint8

const (
	Unopened Context = iota
	Limped
	SingleOpen
	MultipleOpen
	Contexts
)

type Percentages [Positions][Contexts]float64

// DefaultPercentages gates only entry. A learned policy chooses call/check or
// raise inside the admitted range; a free big-blind option always checks when
// excluded.
var DefaultPercentages = Percentages{
	Button:      {45, 45, 22, 10},
	SmallBlind:  {45, 40, 18, 8},
	BigBlind:    {0, 40, 30, 12},
	UnderTheGun: {20, 20, 12, 8},
	Hijack:      {25, 25, 15, 9},
	Cutoff:      {35, 35, 20, 10},
}

type Gate struct {
	fractions [Positions][Contexts][handclass.Num]float64
	admitted  [Positions][Contexts]int
}

func NewGate(ranking *Ranking, percentages Percentages) (*Gate, error) {
	if err := ranking.Validate(); err != nil {
		return nil, err
	}
	ordered := append([]ClassEV(nil), ranking.Classes...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].BestEV == ordered[j].BestEV {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].BestEV > ordered[j].BestEV
	})
	g := &Gate{}
	for position := Position(0); position < Positions; position++ {
		for context := Context(0); context < Contexts; context++ {
			pct := percentages[position][context]
			if !finite(pct) || pct < 0 || pct > 100 {
				return nil, fmt.Errorf("garnet gate: invalid %.4g%% at position %d context %d", pct, position, context)
			}
			target := int(math.Round(float64(cards.Deals) * pct / 100))
			remaining := target
			for _, class := range ordered {
				if remaining <= 0 {
					break
				}
				admit := min(remaining, class.Weight)
				g.fractions[position][context][class.ID] = float64(admit) / float64(class.Weight)
				remaining -= admit
			}
			g.admitted[position][context] = target - remaining
		}
	}
	return g, nil
}

func (g *Gate) AdmittedDeals(position Position, context Context) int {
	if g == nil || position >= Positions || context >= Contexts {
		return 0
	}
	return g.admitted[position][context]
}

func (g *Gate) ClassFraction(position Position, context Context, id handclass.ID) float64 {
	if g == nil || position >= Positions || context >= Contexts || id >= handclass.Num {
		return 0
	}
	return g.fractions[position][context][id]
}

// Admit deterministically splits a boundary class by its physical suit deal.
// Repeated predraw decisions for the same hand therefore never resample entry.
func (g *Gate) Admit(position Position, context Context, hand []cards.Card) bool {
	if len(hand) != 5 || cards.NewSet(hand).Len() != 5 {
		return false
	}
	id := handclass.Of(hand)
	fraction := g.ClassFraction(position, context, id)
	if fraction <= 0 {
		return false
	}
	if fraction >= 1 {
		return true
	}
	ordinal, ok := physicalOrdinal(hand)
	if !ok {
		return false
	}
	cutoff := int(math.Round(fraction * float64(handclass.Weight(id))))
	return ordinal < cutoff
}

func physicalOrdinal(hand []cards.Card) (int, bool) {
	if len(hand) != 5 || cards.NewSet(hand).Len() != 5 {
		return 0, false
	}
	sorted := cards.SortedByRank(hand)
	if cards.SameSuit(sorted) {
		return sorted[0].Index() % 4, true
	}
	if len(cards.DistinctRanks(sorted)) == 5 {
		raw := 0
		for _, card := range sorted {
			raw = raw*4 + card.Index()%4
		}
		// Remove the four flush assignments from the mixed-suit ordinal.
		excluded := 0
		for suit := 0; suit < 4; suit++ {
			flushCode := suit * (1 + 4 + 16 + 64 + 256)
			if flushCode < raw {
				excluded++
			}
		}
		return raw - excluded, true
	}
	ordinal := 0
	for start := 0; start < len(sorted); {
		end, mask := start, 0
		for end < len(sorted) && sorted[end].Rank == sorted[start].Rank {
			mask |= 1 << (sorted[end].Index() % 4)
			end++
		}
		count := end - start
		choice, choices := suitCombinationOrdinal(mask, count)
		if choice < 0 {
			return 0, false
		}
		ordinal = ordinal*choices + choice
		start = end
	}
	return ordinal, true
}

func suitCombinationOrdinal(mask, count int) (int, int) {
	ordinal, choices := -1, 0
	for candidate := 0; candidate < 16; candidate++ {
		if bits.OnesCount8(uint8(candidate)) != count {
			continue
		}
		if candidate == mask {
			ordinal = choices
		}
		choices++
	}
	return ordinal, choices
}
