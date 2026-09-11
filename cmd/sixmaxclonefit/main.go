// Command sixmaxclonefit fits the default-off predraw Swit2/3 imitation model.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func main() {
	dataRoot := flag.String("data", "bin/sixmax/data", "existing collected match root")
	matches := flag.String("matches", "36,37,38,39,41,77,84,85,101,102,1081,1086", "existing-data prototype match ids; match 103 is forbidden")
	trainDirs := flag.String("train-dirs", "", "comma-separated explicit training roots or match directories")
	validationDirs := flag.String("validation-dirs", "", "comma-separated explicit validation roots or match directories")
	trainingMatches := flag.String("training-matches", "", "existing match ids to add wholly to explicit training")
	developmentMatches := flag.String("development-matches", "", "existing matches to split by deal fingerprint and add to explicit train/validation")
	out := flag.String("out", "bin/sixmax/models/clone-predraw.json", "selected model output")
	metricsPath := flag.String("metrics", "bin/sixmax/models/clone-predraw-metrics.json", "validation metrics output")
	sourceSnapshotPath := flag.String("source-snapshot", "", "optional stable source selection JSON")
	flag.Parse()

	var train, validation []trainingRow
	var err error
	var sources []string
	explicitTrain, explicitValidation := splitList(*trainDirs), splitList(*validationDirs)
	if len(explicitTrain) > 0 || len(explicitValidation) > 0 {
		if len(explicitTrain) == 0 || len(explicitValidation) == 0 {
			fatal(fmt.Errorf("explicit fitting requires both -train-dirs and -validation-dirs"))
		}
		train, err = loadPaths(explicitTrain)
		if err == nil {
			validation, err = loadPaths(explicitValidation)
		}
		sources = append(append([]string{}, explicitTrain...), explicitValidation...)
		if *trainingMatches != "" {
			trainingRows, trainingSources, loadErr := loadWholeMatches(*dataRoot, *trainingMatches)
			if loadErr != nil {
				fatal(loadErr)
			}
			train = append(train, trainingRows...)
			sources = append(sources, trainingSources...)
		}
		if *developmentMatches != "" {
			ids, parseErr := parseIDs(*developmentMatches)
			if parseErr != nil {
				fatal(parseErr)
			}
			devRows, loadErr := loadExisting(*dataRoot, ids)
			if loadErr != nil {
				fatal(loadErr)
			}
			devRows, loadErr = deduplicateAndWeight(devRows)
			if loadErr != nil {
				fatal(loadErr)
			}
			devTrain, devValidation := splitByFingerprint(devRows)
			train, validation = append(train, devTrain...), append(validation, devValidation...)
			for _, id := range ids {
				sources = append(sources, fmt.Sprintf("development:match-%d", id))
			}
		}
	} else {
		ids, parseErr := parseIDs(*matches)
		if parseErr != nil {
			fatal(parseErr)
		}
		rows, loadErr := loadExisting(*dataRoot, ids)
		if loadErr != nil {
			fatal(loadErr)
		}
		for _, id := range ids {
			sources = append(sources, fmt.Sprintf("match-%d", id))
		}
		rows, err = deduplicateAndWeight(rows)
		if err != nil {
			fatal(err)
		}
		train, validation = splitByFingerprint(rows)
	}
	if err != nil {
		fatal(err)
	}
	train, err = deduplicateAndWeight(train)
	if err == nil {
		validation, err = deduplicateAndWeight(validation)
	}
	if err != nil {
		fatal(err)
	}
	if err := assertDisjoint(train, validation); err != nil {
		fatal(err)
	}
	model, err := fitModel(train)
	if err != nil {
		fatal(err)
	}
	selected, thresholds, selectionErr := selectConfidence(model, validation)
	report := fitMetrics{Schema: 1, Source: model.Source, TrainingRows: len(train), TrainingGroups: distinctGroups(train),
		ValidationRows: len(validation), ValidationGroups: distinctGroups(validation), Sources: sources,
		SelectedConfidence: selected, Thresholds: thresholds}
	if err := writeJSON(*metricsPath, report); err != nil {
		fatal(err)
	}
	if *sourceSnapshotPath != "" {
		snapshot := sourceSnapshot{Schema: 1, TrainingSources: explicitTrain, ValidationSources: explicitValidation,
			TrainingMatches: splitList(*trainingMatches), DevelopmentMatches: splitList(*developmentMatches),
			ExcludedMatches: []int{103, 1106, 1107, 1108}}
		if err := writeJSON(*sourceSnapshotPath, snapshot); err != nil {
			fatal(err)
		}
	}
	if selectionErr != nil {
		fatal(selectionErr)
	}
	model.MinConfidence = selected
	if err := writeJSON(*out, model); err != nil {
		fatal(err)
	}
}

func loadWholeMatches(root, text string) ([]trainingRow, []string, error) {
	ids, err := parseIDs(text)
	if err != nil {
		return nil, nil, err
	}
	rows, err := loadExisting(root, ids)
	if err != nil {
		return nil, nil, err
	}
	sources := make([]string, len(ids))
	for i, id := range ids {
		sources[i] = fmt.Sprintf("training:match-%d", id)
	}
	return rows, sources, nil
}

type sourceSnapshot struct {
	Schema             int      `json:"schema"`
	TrainingSources    []string `json:"trainingSources"`
	ValidationSources  []string `json:"validationSources"`
	TrainingMatches    []string `json:"trainingMatches,omitempty"`
	DevelopmentMatches []string `json:"developmentMatches"`
	ExcludedMatches    []int    `json:"excludedMatches"`
}

func loadExisting(root string, ids []int) ([]trainingRow, error) {
	var rows []trainingRow
	for _, id := range ids {
		if id == 103 {
			return nil, fmt.Errorf("locked match 103 cannot be used by clone fitter")
		}
		got, err := loadMatch(filepath.Join(root, fmt.Sprintf("match-%d", id)))
		if err != nil {
			return nil, err
		}
		rows = append(rows, got...)
	}
	return rows, nil
}

func loadPaths(paths []string) ([]trainingRow, error) {
	var rows []trainingRow
	for _, path := range paths {
		if forbiddenTestPath(path) {
			return nil, fmt.Errorf("locked test path %s cannot be used by clone fitter", path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", path)
		}
		if strings.HasPrefix(filepath.Base(path), "match-") {
			got, err := loadMatch(path)
			if err != nil {
				return nil, err
			}
			rows = append(rows, got...)
			continue
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "match-") {
				continue
			}
			got, err := loadMatch(filepath.Join(path, entry.Name()))
			if err != nil {
				return nil, err
			}
			rows = append(rows, got...)
		}
	}
	return rows, nil
}

func forbiddenTestPath(path string) bool {
	clean := filepath.Clean(path)
	for {
		base := filepath.Base(clean)
		if strings.EqualFold(base, "test") || strings.EqualFold(base, "lockedtest") {
			return true
		}
		parent := filepath.Dir(clean)
		if parent == clean {
			return false
		}
		clean = parent
	}
}

func parseIDs(text string) ([]int, error) {
	var ids []int
	seen := map[int]bool{}
	for _, part := range strings.Split(text, ",") {
		id, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid match id %q", part)
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids, nil
}

func splitList(text string) []string {
	var values []string
	for _, part := range strings.Split(text, ",") {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
