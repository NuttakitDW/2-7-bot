package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nuttakit/2-7-bot/internal/cfr"
)

func refine(args []string) error {
	fs := flag.NewFlagSet("refine", flag.ContinueOnError)
	in := fs.String("in", "", "legacy compact raw checkpoint")
	out := fs.String("out", "", "refined raw checkpoint")
	bp := fs.String("bp", "", "refined blueprint")
	history := fs.Bool("history", false, "source uses the complete betting-history layout")
	drawing := fs.Bool("draw-hands", false, "with -history: refine early-draw hand groups instead of river groups")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *drawing && !*history {
		return fmt.Errorf("draw-hands requires history")
	}
	if err := distinctRefinePaths(*in, *out, *bp); err != nil {
		return err
	}
	refine, profile := cfr.RefineCompactState, "compact-rich"
	if *history {
		refine, profile = cfr.RefineHistoryState, "history-rich"
	}
	if *drawing {
		refine, profile = cfr.RefineDrawingState, "history with -X github.com/nuttakit/2-7-bot/internal/cfr.handProfile=draw-shape"
	}
	tr, err := refine(*in)
	if err != nil {
		return err
	}
	fmt.Printf("refined %d iterations into %d early-draw/%d river buckets, %d betting slots; train using %s\n", tr.Iterations(), tr.Abs.DrawBuckets, tr.Abs.FinalBuckets, tr.Layout.BetSlots, profile)
	return save(tr, *bp, *out, 1)
}

func distinctRefinePaths(paths ...string) error {
	resolved := make([]string, 0, len(paths))
	infos := make([]os.FileInfo, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			return fmt.Errorf("in, out and bp must be distinct, nonempty paths")
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		canonical, err := filepath.EvalSymlinks(absolute)
		if os.IsNotExist(err) {
			var parent string
			parent, err = filepath.EvalSymlinks(filepath.Dir(absolute))
			canonical = filepath.Join(parent, filepath.Base(absolute))
		}
		if err != nil {
			return err
		}
		info, err := os.Stat(absolute)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		for i, prior := range resolved {
			if canonical == prior || (info != nil && infos[i] != nil && os.SameFile(info, infos[i])) {
				return fmt.Errorf("in, out and bp must be distinct paths")
			}
		}
		resolved = append(resolved, canonical)
		infos = append(infos, info)
	}
	return nil
}
