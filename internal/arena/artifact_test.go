package arena

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectArtifactRejectsNonELF(t *testing.T) {
	dir := t.TempDir()

	script := filepath.Join(dir, "bot.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec python3 bot.py\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	info, err := InspectArtifact(script)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if info.OK() {
		t.Fatal("a shell script must be rejected — the arena refuses scripts and shell bundles")
	}
	if !strings.Contains(strings.Join(info.Problems, " "), "not an ELF") {
		t.Errorf("unexpected problems: %v", info.Problems)
	}
	if info.Digest == "" {
		t.Error("digest must be computed even for a rejected artifact")
	}
}

func TestInspectArtifactRejectsEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	info, err := InspectArtifact(path)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if info.OK() {
		t.Fatal("an empty file must be rejected")
	}
}

func TestInspectArtifactZipEntryCount(t *testing.T) {
	tests := []struct {
		name    string
		entries []string
		wantOK  bool
	}{
		{"single entry", []string{"bot"}, true},
		{"two entries", []string{"bot", "weights.bin"}, false},
		{"no entries", nil, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "artifact.zip")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			writer := zip.NewWriter(file)
			for _, name := range test.entries {
				entry, err := writer.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write([]byte("payload")); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			file.Close()

			info, err := InspectArtifact(path)
			if err != nil {
				t.Fatalf("inspect: %v", err)
			}
			if info.Kind != KindZIP {
				t.Fatalf("kind = %q, want zip", info.Kind)
			}
			if info.OK() != test.wantOK {
				t.Errorf("OK() = %t, want %t (problems: %v)", info.OK(), test.wantOK, info.Problems)
			}
		})
	}
}

// The positive case is also a check on our own build recipe: whatever
// `make build-bot` will eventually emit must satisfy the artifact contract.
func TestInspectArtifactAcceptsStaticLinuxAmd64Build(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiles a binary")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable")
	}

	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	if err := os.WriteFile(source, []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "bot")

	build := exec.Command("go", "build", "-o", binary, source)
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("cross-compile unavailable: %v: %s", err, out)
	}

	info, err := InspectArtifact(binary)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !info.OK() {
		t.Fatalf("a static linux/amd64 Go build must be accepted, problems: %v", info.Problems)
	}
	if !info.Static {
		t.Error("CGO_ENABLED=0 build reported as dynamically linked")
	}
	if info.Machine != "EM_X86_64" {
		t.Errorf("machine = %q", info.Machine)
	}
	if len(info.Digest) != 64 {
		t.Errorf("digest = %q, want 64 hex characters", info.Digest)
	}
}
