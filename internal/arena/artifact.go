package arena

import (
	"archive/zip"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// MaxArtifactBytes is the platform's upload ceiling: 300 MiB.
const MaxArtifactBytes int64 = 300 * 1024 * 1024

// Artifact kinds the platform accepts.
const (
	KindELF = "elf"
	KindZIP = "zip"
)

// ArtifactInfo is the result of inspecting a candidate upload.
//
// The arena accepts exactly one shape — a static Linux x86-64 ELF, or a ZIP
// holding exactly one of those — and rejects everything else during
// validation, which costs a round trip and a smoke-test slot. Checking locally
// is cheap, so we do it first. See docs/arena/hosted-bot-interface.md.
type ArtifactInfo struct {
	Path     string
	Kind     string
	Size     int64
	Digest   string
	Machine  string
	Class    string
	Static   bool
	Problems []string
}

// OK reports whether the artifact satisfies every rule we can check locally.
func (a *ArtifactInfo) OK() bool { return len(a.Problems) == 0 }

// InspectArtifact hashes a candidate upload and checks it against the platform's
// artifact contract.
func InspectArtifact(path string) (*ArtifactInfo, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat artifact: %w", err)
	}
	if stat.IsDir() {
		return nil, fmt.Errorf("%s is a directory; the arena accepts one executable or one ZIP", path)
	}

	digest, err := fileDigest(path)
	if err != nil {
		return nil, err
	}

	info := &ArtifactInfo{Path: path, Size: stat.Size(), Digest: digest}
	if info.Size == 0 {
		info.Problems = append(info.Problems, "file is empty")
		return info, nil
	}
	if info.Size > MaxArtifactBytes {
		info.Problems = append(info.Problems,
			fmt.Sprintf("size %d exceeds the 300 MiB limit", info.Size))
	}

	switch {
	case isZip(path):
		info.Kind = KindZIP
		inspectZip(info)
	default:
		info.Kind = KindELF
		inspectELF(path, info)
	}
	return info, nil
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash artifact: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func isZip(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	magic := make([]byte, 4)
	if _, err := io.ReadFull(file, magic); err != nil {
		return false
	}
	if magic[0] != 'P' || magic[1] != 'K' {
		return false
	}
	// Local file header, end-of-central-directory (an archive with no entries),
	// or a spanned archive marker. All three are ZIPs as far as classification
	// goes; whether they are *acceptable* is inspectZip's business.
	switch {
	case magic[2] == 3 && magic[3] == 4:
		return true
	case magic[2] == 5 && magic[3] == 6:
		return true
	case magic[2] == 7 && magic[3] == 8:
		return true
	}
	return false
}

// inspectELF verifies the platform's three hard requirements: ELF, x86-64, and
// statically linked.
func inspectELF(path string, info *ArtifactInfo) {
	file, err := elf.Open(path)
	if err != nil {
		info.Problems = append(info.Problems,
			"not an ELF executable (the arena rejects Mach-O binaries, scripts and shell wrappers)")
		return
	}
	defer file.Close()

	info.Machine = file.Machine.String()
	info.Class = file.Class.String()

	if file.Machine != elf.EM_X86_64 {
		info.Problems = append(info.Problems,
			fmt.Sprintf("architecture is %s, need x86-64 — build with GOARCH=amd64", file.Machine))
	}
	if file.Class != elf.ELFCLASS64 {
		info.Problems = append(info.Problems, fmt.Sprintf("ELF class is %s, need 64-bit", file.Class))
	}

	// A PT_INTERP program header names a dynamic loader, which the sandbox has
	// no way to satisfy. CGO_ENABLED=0 is what removes it from a Go build.
	info.Static = true
	for _, prog := range file.Progs {
		if prog.Type == elf.PT_INTERP {
			info.Static = false
			break
		}
	}
	if !info.Static {
		info.Problems = append(info.Problems,
			"dynamically linked — build with CGO_ENABLED=0 for a static executable")
	}
}

// inspectZip enforces "exactly one executable, no directories, no links".
func inspectZip(info *ArtifactInfo) {
	reader, err := zip.OpenReader(info.Path)
	if err != nil {
		info.Problems = append(info.Problems, fmt.Sprintf("unreadable ZIP: %v", err))
		return
	}
	defer reader.Close()

	if len(reader.File) != 1 {
		info.Problems = append(info.Problems,
			fmt.Sprintf("ZIP holds %d entries; the arena requires exactly one executable", len(reader.File)))
		return
	}

	entry := reader.File[0]
	if entry.FileInfo().IsDir() {
		info.Problems = append(info.Problems, "ZIP entry is a directory")
	}
	if entry.Mode()&os.ModeSymlink != 0 {
		info.Problems = append(info.Problems, "ZIP entry is a symbolic link")
	}
	// Bit 0 of the general-purpose flags marks an encrypted entry; the
	// standard library exposes no helper for it.
	if entry.Flags&0x1 != 0 {
		info.Problems = append(info.Problems, "ZIP entry is encrypted")
	}
	if int64(entry.UncompressedSize64) > MaxArtifactBytes {
		info.Problems = append(info.Problems, "ZIP entry expands beyond the 300 MiB limit")
	}
}
