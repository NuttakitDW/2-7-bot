// Package sixmaxrange fits and queries public-information opponent ranges for
// closing river calls in six-player 2-7 triple draw.
package sixmaxrange

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/nuttakit/2-7-bot/internal/deuce"
)

const SchemaVersion = 1

type DrawBucket string

const (
	DrawPat     DrawBucket = "0"
	DrawOne     DrawBucket = "1"
	DrawTwoPlus DrawBucket = "2+"
	None                   = "none"
	Aggressive             = "aggressive"
)

type PublicAction struct {
	Seat, Street int
	Action       string
}

type Context struct {
	FinalDraw     DrawBucket `json:"finalDraw"`
	PriorPat      bool       `json:"priorPat"`
	LastAction    string     `json:"lastAction"`
	RaisedEarlier bool       `json:"raisedEarlier"`
	Multiway      bool       `json:"multiway"`
	ActionFamily  string     `json:"actionFamily"`
}

func BuildContext(target int, draws [6][3]int, actions []PublicAction, active int) (Context, bool) {
	if target < 0 || target >= len(draws) || draws[target][2] < 0 {
		return Context{}, false
	}
	c := Context{FinalDraw: bucket(draws[target][2]), PriorPat: draws[target][1] == 0, LastAction: "none", Multiway: active > 2}
	for _, a := range actions {
		if a.Seat == target && a.Street == 3 && a.Action == "raise" {
			c.RaisedEarlier = true
		}
		if a.Seat == target && a.Street == 3 && a.Action != "draw" {
			c.LastAction = a.Action
		}
	}
	c.ActionFamily = actionFamily(c.LastAction)
	return c, true
}

func bucket(n int) DrawBucket {
	switch n {
	case 0:
		return DrawPat
	case 1:
		return DrawOne
	default:
		return DrawTwoPlus
	}
}

func actionFamily(action string) string {
	if action == "bet" || action == "raise" {
		return Aggressive
	}
	return action
}

type RankMass struct {
	Value deuce.Value `json:"value"`
	Mass  float64     `json:"mass"`
}
type Distribution struct {
	Ranks []RankMass `json:"ranks"`
}

// CDF returns P(opponent is strictly worse) and P(tie). Greater values are
// stronger in deuce.Value, so worse values lie below hero.
func (d Distribution) CDF(hero deuce.Value) (less, equal float64) {
	for _, point := range d.Ranks {
		if point.Value < hero {
			less += point.Mass
		} else if point.Value == hero {
			equal = point.Mass
			break
		} else {
			break
		}
	}
	return less, equal
}

// ShowdownEquity computes hero's expected pot share against up to five
// independent marginals. The polynomial coefficient at k is the probability
// all opponents are worse or tied and exactly k tie hero.
func ShowdownEquity(hero deuce.Value, opponents []Distribution) float64 {
	if len(opponents) > 5 {
		return 0
	}
	poly := []float64{1}
	for _, d := range opponents {
		less, equal := d.CDF(hero)
		next := make([]float64, len(poly)+1)
		for k, p := range poly {
			next[k] += p * less
			next[k+1] += p * equal
		}
		poly = next
	}
	equity := 0.0
	for ties, probability := range poly {
		equity += probability / float64(ties+1)
	}
	return equity
}

type Cell struct {
	Level           string       `json:"level"`
	Key             string       `json:"key"`
	EffectiveGroups float64      `json:"effectiveGroups"`
	Distribution    Distribution `json:"distribution"`
}

type Model struct {
	Schema int    `json:"schema"`
	Source string `json:"source"`
	Cells  []Cell `json:"cells"`
}

func (m Model) Lookup(c Context) (Distribution, bool) {
	for _, level := range []string{"full", "draw-action", "pat-action", "action"} {
		key := c.key(level)
		for _, cell := range m.Cells {
			if cell.Level == level && cell.Key == key {
				return cell.Distribution, true
			}
		}
	}
	return Distribution{}, false
}

func (c Context) key(level string) string {
	switch level {
	case "full":
		return fmt.Sprintf("%s|%t|%s|%t|%t", c.FinalDraw, c.PriorPat, c.LastAction, c.RaisedEarlier, c.Multiway)
	case "draw-action":
		return fmt.Sprintf("%s|%s", c.FinalDraw, c.LastAction)
	case "pat-action":
		return fmt.Sprintf("%t|%s", c.FinalDraw == DrawPat, c.ActionFamily)
	default:
		return c.ActionFamily
	}
}

type Row struct {
	MatchID int
	Hand    int
	Group   string
	Target  int
	Context Context
	Value   deuce.Value
	Weight  float64
}

type FitConfig struct{ FullMinGroups, BroadMinGroups, Shrink float64 }

func DefaultFitConfig() FitConfig { return FitConfig{8, 20, 20} }

type accumulator struct {
	masses map[deuce.Value]float64
	groups map[string]float64
}

func Fit(rows []Row, cfg FitConfig) (Model, error) {
	m := Model{Schema: SchemaVersion, Source: "pooled strong six-max opponents"}
	if err := validateFitConfig(cfg); err != nil {
		return m, err
	}
	levels := []string{"action", "pat-action", "draw-action", "full"}
	byLevel := map[string]map[string]*accumulator{}
	seen := map[string]map[string]bool{}
	for _, level := range levels {
		byLevel[level] = map[string]*accumulator{}
		seen[level] = map[string]bool{}
	}
	for _, row := range rows {
		if row.Weight <= 0 {
			continue
		}
		group := row.Group
		if group == "" {
			group = fmt.Sprintf("%d:%d", row.MatchID, row.Hand)
		}
		for _, level := range levels {
			key := row.Context.key(level)
			id := fmt.Sprintf("%d|%d|%d|%s", row.MatchID, row.Hand, row.Target, key)
			if seen[level][id] {
				continue
			}
			seen[level][id] = true
			a := byLevel[level][key]
			if a == nil {
				a = &accumulator{masses: map[deuce.Value]float64{}, groups: map[string]float64{}}
				byLevel[level][key] = a
			}
			a.masses[row.Value] += row.Weight
			a.groups[group] += row.Weight
		}
	}
	stored := map[string]Distribution{}
	for _, level := range levels {
		keys := make([]string, 0, len(byLevel[level]))
		for key := range byLevel[level] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			a := byLevel[level][key]
			eff := effective(a.groups)
			minimum := cfg.BroadMinGroups
			if level == "full" {
				minimum = cfg.FullMinGroups
			}
			if eff < minimum {
				continue
			}
			d := empirical(a.masses)
			if level != "action" {
				ctx := contextFromKey(level, key)
				parentLevel := map[string]string{"pat-action": "action", "draw-action": "action", "full": "draw-action"}[level]
				parent, ok := stored[parentLevel+"\x00"+ctx.key(parentLevel)]
				if !ok {
					continue
				}
				lambda := eff / (eff + cfg.Shrink)
				d = blend(d, parent, lambda)
			}
			stored[level+"\x00"+key] = d
			m.Cells = append(m.Cells, Cell{Level: level, Key: key, EffectiveGroups: eff, Distribution: d})
		}
	}
	return m, nil
}

func validateFitConfig(cfg FitConfig) error {
	if cfg.FullMinGroups <= 0 || cfg.BroadMinGroups <= 0 || cfg.Shrink < 0 ||
		math.IsNaN(cfg.FullMinGroups) || math.IsInf(cfg.FullMinGroups, 0) ||
		math.IsNaN(cfg.BroadMinGroups) || math.IsInf(cfg.BroadMinGroups, 0) ||
		math.IsNaN(cfg.Shrink) || math.IsInf(cfg.Shrink, 0) {
		return fmt.Errorf("invalid fit thresholds")
	}
	return nil
}

func effective(groups map[string]float64) float64 {
	var sum, squares float64
	for _, w := range groups {
		sum += w
		squares += w * w
	}
	if squares == 0 {
		return 0
	}
	return sum * sum / squares
}

func empirical(masses map[deuce.Value]float64) Distribution {
	values := make([]int, 0, len(masses))
	total := 0.0
	for v, w := range masses {
		values = append(values, int(v))
		total += w
	}
	sort.Ints(values)
	d := Distribution{Ranks: make([]RankMass, 0, len(values))}
	for _, raw := range values {
		v := deuce.Value(raw)
		d.Ranks = append(d.Ranks, RankMass{v, masses[v] / total})
	}
	return d
}

func blend(child, parent Distribution, lambda float64) Distribution {
	m := map[deuce.Value]float64{}
	for _, p := range child.Ranks {
		m[p.Value] += lambda * p.Mass
	}
	for _, p := range parent.Ranks {
		m[p.Value] += (1 - lambda) * p.Mass
	}
	return empirical(m)
}

func contextFromKey(level, key string) Context {
	parts := strings.Split(key, "|")
	c := Context{}
	switch level {
	case "pat-action":
		if parts[0] == "true" {
			c.FinalDraw = DrawPat
		} else {
			c.FinalDraw = DrawOne
		}
		c.ActionFamily = parts[1]
	case "draw-action":
		c.FinalDraw = DrawBucket(parts[0])
		c.LastAction = parts[1]
		c.ActionFamily = actionFamily(c.LastAction)
	case "full":
		c.FinalDraw, c.PriorPat, c.LastAction = DrawBucket(parts[0]), parts[1] == "true", parts[2]
		c.RaisedEarlier, c.Multiway, c.ActionFamily = parts[3] == "true", parts[4] == "true", actionFamily(c.LastAction)
	}
	return c
}

func Decode(data []byte) (*Model, error) {
	var m Model
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.Schema != SchemaVersion || m.Source == "" {
		return nil, fmt.Errorf("invalid model metadata")
	}
	seen := map[string]bool{}
	for _, cell := range m.Cells {
		id := cell.Level + "\x00" + cell.Key
		if seen[id] || cell.EffectiveGroups <= 0 || math.IsNaN(cell.EffectiveGroups) || math.IsInf(cell.EffectiveGroups, 0) || len(cell.Distribution.Ranks) == 0 || !validCellKey(cell.Level, cell.Key) {
			return nil, fmt.Errorf("invalid cell %q", cell.Key)
		}
		seen[id] = true
		total := 0.0
		var previous deuce.Value
		for i, p := range cell.Distribution.Ranks {
			if uint32(p.Value) > 0x00ffffff || p.Mass < 0 || math.IsNaN(p.Mass) || math.IsInf(p.Mass, 0) || (i > 0 && p.Value <= previous) {
				return nil, fmt.Errorf("invalid distribution %q", cell.Key)
			}
			previous, total = p.Value, total+p.Mass
		}
		if math.Abs(total-1) > 1e-9 {
			return nil, fmt.Errorf("distribution %q sums to %g", cell.Key, total)
		}
	}
	return &m, nil
}

func validCellKey(level, key string) bool {
	parts := strings.Split(key, "|")
	validBool := func(s string) bool { return s == "true" || s == "false" }
	validDraw := func(s string) bool { return s == string(DrawPat) || s == string(DrawOne) || s == string(DrawTwoPlus) }
	validAction := func(s string) bool { return s == "none" || s == "check" || s == "call" || s == "bet" || s == "raise" }
	validFamily := func(s string) bool { return s == "none" || s == "check" || s == "call" || s == Aggressive }
	switch level {
	case "action":
		return len(parts) == 1 && validFamily(parts[0])
	case "pat-action":
		return len(parts) == 2 && validBool(parts[0]) && validFamily(parts[1])
	case "draw-action":
		return len(parts) == 2 && validDraw(parts[0]) && validAction(parts[1])
	case "full":
		return len(parts) == 5 && validDraw(parts[0]) && validBool(parts[1]) && validAction(parts[2]) && validBool(parts[3]) && validBool(parts[4])
	default:
		return false
	}
}
