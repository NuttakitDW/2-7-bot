package main

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nuttakit/2-7-bot/internal/arena"
)

func uploadCommand() command {
	return command{
		name:    "upload",
		summary: "upload an artifact as a new version of a bot",
		usage:   "arena upload [--name NAME] --games GAME --counts N[,N] --file PATH [--dry-run] [--append] [--force]",
		needKey: true,
		run:     runUpload,
	}
}

func runUpload(ctx context.Context, client *arena.Client, args []string) error {
	flags := flag.NewFlagSet("upload", flag.ContinueOnError)
	name := flags.String("name", "", "bot name; defaults to the artifact filename (see docs/naming.md)")
	games := flags.String("games", "", "comma-separated game ids, e.g. 27td-fl")
	counts := flags.String("counts", "", "comma-separated exact table sizes, e.g. 2,6")
	path := flags.String("file", "", "artifact: a static linux/amd64 ELF, or a ZIP holding one")
	dryRun := flags.Bool("dry-run", false, "inspect and plan without calling the API")
	appendVersion := flags.Bool("append", false, "append a version to an existing name — only to replace a broken build")
	force := flags.Bool("force", false, "upload even if local pre-flight checks fail")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if *games == "" || *counts == "" || *path == "" {
		return fmt.Errorf("usage: %s", uploadCommand().usage)
	}

	// The convention has a built artifact carry the name it will be uploaded
	// under, so deriving one from the other removes a class of wrong-binary
	// mistakes rather than inventing a default.
	derived := *name == ""
	if derived {
		*name = botNameFromPath(*path)
	}

	// Parse before inspecting: InspectArtifact hashes up to 300 MiB, and a
	// mistyped name should not cost that read.
	parsed, err := arena.ParseBotName(*name)
	if err != nil {
		return err
	}

	playerCounts, err := parseCounts(*counts)
	if err != nil {
		return err
	}

	info, err := arena.InspectArtifact(*path)
	if err != nil {
		return err
	}
	printArtifact(info)

	request := arena.UploadRequest{
		Name:         *name,
		Games:        splitCSV(*games),
		PlayerCounts: playerCounts,
		Size:         info.Size,
	}
	if err := request.Validate(); err != nil {
		return err
	}

	fmt.Println()
	if derived {
		fmt.Printf("name       %s (from the artifact filename)\n", request.Name)
	}
	fmt.Printf("declaring  %s for %s at %s\n",
		request.Name, strings.Join(request.Games, ","), joinInts(request.PlayerCounts))
	fmt.Printf("smoke      %d validation match(es) will run before the version is selectable\n",
		len(request.Games)*len(request.PlayerCounts))

	if !info.OK() && !*force {
		return fmt.Errorf("artifact fails local pre-flight; fix it or pass --force to upload anyway")
	}

	if *dryRun {
		chunks := arena.PlanChunks(info.Size, arena.DefaultChunkBytes)
		fmt.Printf("plan       %d chunk(s) of up to %s (server's chunkBytes wins)\n",
			chunks, humanBytes(arena.DefaultChunkBytes))
		fmt.Println("note       the existing-name check needs the API; --dry-run stays offline")
		fmt.Println("dry run — nothing was sent")
		return nil
	}

	existing, err := client.FindBotByName(ctx, request.Name)
	if err != nil {
		return err
	}
	if existing != nil && !*appendVersion {
		return fmt.Errorf("%q already exists — a raceable build takes a new name, e.g. %s (see docs/naming.md); "+
			"pass --append only to replace a broken build under the same name", request.Name, parsed.NextGen())
	}
	if existing != nil {
		fmt.Printf("note       %q exists; appending a version to it\n", request.Name)
	}

	fmt.Println()
	result, err := client.UploadArtifact(ctx, request, *path, printProgress)
	if err != nil {
		return err
	}
	fmt.Printf("\nuploaded — server response: %s\n", strings.TrimSpace(string(result)))
	fmt.Println("validation runs asynchronously; poll `arena bots` until the version is ready")
	return nil
}

// botNameFromPath derives the bot name from the artifact filename, minus the
// ZIP wrapper the platform also accepts.
func botNameFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".zip")
}

func printArtifact(info *arena.ArtifactInfo) {
	fmt.Printf("artifact   %s\n", info.Path)
	fmt.Printf("kind       %s", info.Kind)
	if info.Machine != "" {
		linkage := "dynamic"
		if info.Static {
			linkage = "static"
		}
		fmt.Printf(" (%s, %s, %s)", info.Machine, info.Class, linkage)
	}
	fmt.Println()
	fmt.Printf("size       %s (%d bytes)\n", humanBytes(info.Size), info.Size)
	fmt.Printf("sha256     %s\n", info.Digest)

	for _, problem := range info.Problems {
		fmt.Printf("PROBLEM    %s\n", problem)
	}
}

// printProgress reports upload progress. Most artifacts are a single chunk, so
// this stays quiet unless there is genuinely something to watch.
func printProgress(sent, total int64) {
	if total <= arena.DefaultChunkBytes {
		return
	}
	fmt.Printf("\rsending    %s / %s (%.0f%%)", humanBytes(sent), humanBytes(total),
		float64(sent)/float64(total)*100)
	if sent == total {
		fmt.Println()
	}
}

func parseCounts(raw string) ([]int, error) {
	parts := splitCSV(raw)
	counts := make([]int, 0, len(parts))
	for _, part := range parts {
		count, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("table size %q is not a number", part)
		}
		counts = append(counts, count)
	}
	return counts, nil
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
