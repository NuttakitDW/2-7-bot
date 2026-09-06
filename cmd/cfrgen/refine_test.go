package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDrawingRefineRequiresHistory(t *testing.T) {
	if err := refine([]string{"-draw-hands"}); err == nil || !strings.Contains(err.Error(), "requires history") {
		t.Fatalf("expected profile error, got %v", err)
	}
}

func TestRefineRejectsPathAliases(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "state")
	if err := os.WriteFile(src, []byte("checkpoint"), 0600); err != nil {
		t.Fatal(err)
	}
	link, hard := filepath.Join(dir, "symlink"), filepath.Join(dir, "hardlink")
	if err := os.Symlink(src, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(src, hard); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{dir + "/./state", link, hard} {
		if err := distinctRefinePaths(src, alias, filepath.Join(dir, "bp")); err == nil {
			t.Fatalf("accepted source alias %q", alias)
		}
	}
	if err := distinctRefinePaths(src, filepath.Join(dir, "new-state"), filepath.Join(dir, "bp")); err != nil {
		t.Fatal(err)
	}
}
