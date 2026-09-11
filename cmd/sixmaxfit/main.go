// Command sixmaxfit fits a pooled strong-opponent closing-river range model.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nuttakit/2-7-bot/internal/sixmaxdata"
	"github.com/nuttakit/2-7-bot/internal/sixmaxrange"
)

type matchFile struct {
	MatchInfo struct {
		ID       int      `json:"id"`
		DealMode string   `json:"dealMode"`
		Players  []string `json:"players"`
	} `json:"matchInfo"`
}
type calibration struct {
	TotalRows       int     `json:"totalRows"`
	SupportedRows   int     `json:"supportedRows"`
	SupportedWeight float64 `json:"supportedWeight"`
	MeanLogLoss     float64 `json:"meanLogLoss"`
	MeanPIT         float64 `json:"meanPIT"`
}
type summary struct {
	Schema             int            `json:"schema"`
	ModelSource        string         `json:"modelSource"`
	Filter             []string       `json:"filter"`
	InputMatches       []int          `json:"inputMatches"`
	CalibrationHoldout int            `json:"calibrationHoldout"`
	TrainingRows       int            `json:"trainingRows"`
	HoldoutRows        int            `json:"holdoutRows"`
	DeploymentRows     int            `json:"deploymentRows"`
	TrainingGroups     float64        `json:"trainingEffectiveGroups"`
	HoldoutGroups      float64        `json:"holdoutEffectiveGroups"`
	CellsByLevel       map[string]int `json:"cellsByLevel"`
	Calibration        calibration    `json:"calibration"`
}

func main() {
	data := flag.String("data", "bin/sixmax/data", "collected match root")
	out := flag.String("out", "bin/sixmax/range-h3.json", "deployment model")
	summaryPath := flag.String("summary", "bin/sixmax/range-h3-summary.json", "fit summary")
	holdout := flag.Int("holdout", 103, "whole-match calibration holdout")
	matches := flag.String("matches", "36,37,38,39,41,85,101,102,103", "comma-separated match ids")
	fullMin := flag.Float64("full-min", 8, "minimum effective groups for full cells")
	broadMin := flag.Float64("broad-min", 20, "minimum effective groups for broad cells")
	shrink := flag.Float64("shrink", 20, "empirical-parent shrink constant")
	flag.Parse()
	ids, err := parseIDs(*matches)
	if err != nil {
		fatal(err)
	}
	rows, err := loadRows(*data, ids)
	if err != nil {
		fatal(err)
	}
	train, held := splitRows(rows, *holdout)
	cfg := sixmaxrange.FitConfig{FullMinGroups: *fullMin, BroadMinGroups: *broadMin, Shrink: *shrink}
	calibrationModel, err := sixmaxrange.Fit(train, cfg)
	if err != nil {
		fatal(err)
	}
	deployment, err := sixmaxrange.Fit(rows, cfg)
	if err != nil {
		fatal(err)
	}
	report := summary{Schema: 1, ModelSource: deployment.Source, Filter: eligibleNames(), InputMatches: ids,
		CalibrationHoldout: *holdout, TrainingRows: len(train), HoldoutRows: len(held), DeploymentRows: len(rows),
		TrainingGroups: effectiveRows(train), HoldoutGroups: effectiveRows(held), CellsByLevel: map[string]int{},
		Calibration: calibrate(calibrationModel, held)}
	for _, cell := range deployment.Cells {
		report.CellsByLevel[cell.Level]++
	}
	if err := writeJSON(*out, deployment); err != nil {
		fatal(err)
	}
	if err := writeJSON(*summaryPath, report); err != nil {
		fatal(err)
	}
}

func loadRows(root string, ids []int) ([]sixmaxrange.Row, error) {
	rows := []sixmaxrange.Row{}
	for _, id := range ids {
		dir := filepath.Join(root, fmt.Sprintf("match-%d", id))
		var meta matchFile
		if err := readJSON(filepath.Join(dir, "match.json"), &meta); err != nil {
			return nil, err
		}
		if meta.MatchInfo.ID != id || (meta.MatchInfo.DealMode != "duplicate" && meta.MatchInfo.DealMode != "seeded") || len(meta.MatchInfo.Players) != 6 {
			return nil, fmt.Errorf("match %d metadata is inconsistent", id)
		}
		var records []sixmaxdata.HandRecord
		if err := readJSON(filepath.Join(dir, "derived.json"), &records); err != nil {
			return nil, err
		}
		for _, record := range records {
			if record.MatchID != id {
				return nil, fmt.Errorf("match %d derived record claims match %d", id, record.MatchID)
			}
			rows = append(rows, rowsFromHand(record, meta.MatchInfo.Players, meta.MatchInfo.DealMode)...)
		}
	}
	return rows, nil
}

func splitRows(rows []sixmaxrange.Row, holdout int) (train, held []sixmaxrange.Row) {
	for _, row := range rows {
		if row.MatchID == holdout {
			held = append(held, row)
		} else {
			train = append(train, row)
		}
	}
	return
}

func calibrate(model sixmaxrange.Model, rows []sixmaxrange.Row) calibration {
	c := calibration{TotalRows: len(rows)}
	totalWeight := 0.0
	for _, row := range rows {
		d, ok := model.Lookup(row.Context)
		if !ok {
			continue
		}
		c.SupportedRows++
		c.SupportedWeight += row.Weight
		totalWeight += row.Weight
		mass, pit := 0.0, 0.0
		for _, p := range d.Ranks {
			if p.Value < row.Value {
				pit += p.Mass
			}
			if p.Value == row.Value {
				mass = p.Mass
				pit += .5 * p.Mass
			}
		}
		c.MeanLogLoss += row.Weight * -math.Log(math.Max(mass, 1e-12))
		c.MeanPIT += row.Weight * pit
	}
	if totalWeight > 0 {
		c.MeanLogLoss /= totalWeight
		c.MeanPIT /= totalWeight
	}
	return c
}

func effectiveRows(rows []sixmaxrange.Row) float64 {
	groups := map[string]float64{}
	for _, row := range rows {
		groups[row.Group] += row.Weight
	}
	sum, sq := 0.0, 0.0
	for _, w := range groups {
		sum += w
		sq += w * w
	}
	if sq == 0 {
		return 0
	}
	return sum * sum / sq
}

func eligibleNames() []string {
	return []string{"swit-27td-ring2", "swit-27td-ring3", "paul-sauron100-neutral-1bit", "paul-sauron100-exploit-1bit", "paul-sauron200-27td-1bit", "paul-sauron300-27td-ring-1bit", "paul-sauron301-27td-ring-1bit"}
}

func parseIDs(text string) ([]int, error) {
	var out []int
	seen := map[int]bool{}
	for _, part := range strings.Split(text, ",") {
		id, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid match id %q", part)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Ints(out)
	return out, nil
}
func readJSON(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
func fatal(err error) { _, _ = fmt.Fprintln(os.Stderr, err); os.Exit(1) }
