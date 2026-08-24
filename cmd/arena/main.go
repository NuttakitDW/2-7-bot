// Command arena is the harness CLI for MixedSolver Arena.
//
// It drives the hosting platform — inspecting bots, uploading artifacts,
// queueing competitions and reading results. It does not play poker; the
// gameplay contract lives in docs/protocol/.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nuttakit/2-7-bot/internal/arena"
)

// command is one subcommand. Commands that only read public data may run
// without a key; the rest resolve one before dispatch.
type command struct {
	name    string
	summary string
	usage   string
	needKey bool
	run     func(ctx context.Context, client *arena.Client, args []string) error
}

func commands() []command {
	return []command{
		healthCommand(),
		botsCommand(),
		versionsCommand(),
		matchesCommand(),
		matchCommand(),
		handsCommand(),
		handCommand(),
		uploadCommand(),
		competitionsCommand(),
		competeCommand(),
		watchCommand(),
	}
}

func main() {
	if err := run(); err != nil {
		var apiErr *arena.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 401 {
			fmt.Fprintf(os.Stderr, "arena: %v\n", err)
			fmt.Fprintf(os.Stderr, "hint: check %s in .env or the environment\n", arena.APIKeyEnv)
		} else {
			fmt.Fprintf(os.Stderr, "arena: %v\n", err)
		}
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		usage()
		return nil
	}

	name, rest := args[0], args[1:]
	for _, cmd := range commands() {
		if cmd.name != name {
			continue
		}

		key := ""
		if cmd.needKey {
			resolved, err := arena.ResolveAPIKey(".env")
			if err != nil {
				return err
			}
			key = resolved
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		return cmd.run(ctx, arena.New(os.Getenv("ARENA_BASE_URL"), key), rest)
	}

	usage()
	return fmt.Errorf("unknown command %q", name)
}

func usage() {
	fmt.Fprintf(os.Stderr, "arena — MixedSolver Arena harness\n\nusage: arena <command> [flags]\n\ncommands:\n")
	for _, cmd := range commands() {
		fmt.Fprintf(os.Stderr, "  %-12s %s\n", cmd.name, cmd.summary)
	}
	fmt.Fprintf(os.Stderr, "\nThe API key is read from %s, or from .env in the working directory.\n", arena.APIKeyEnv)
	fmt.Fprintf(os.Stderr, "Set ARENA_BASE_URL to target a different host (default %s).\n", arena.DefaultBaseURL)
}
