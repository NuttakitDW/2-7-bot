package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/nuttakit/2-7-bot/internal/arena"
)

func competitionsCommand() command {
	return command{
		name:    "competitions",
		summary: "list competitions",
		usage:   "arena competitions [--game GAME] [--json]",
		needKey: true,
		run:     runCompetitions,
	}
}

func runCompetitions(ctx context.Context, client *arena.Client, args []string) error {
	flags := flag.NewFlagSet("competitions", flag.ContinueOnError)
	game := flags.String("game", "", "only competitions for this game")
	asJSON := flags.Bool("json", false, "print raw JSON")
	if err := parseFlags(flags, args); err != nil {
		return err
	}

	competitions, err := client.ListCompetitions(ctx)
	if err != nil {
		return err
	}
	if *game != "" {
		kept := competitions[:0]
		for _, competition := range competitions {
			if competition.Config.Game == *game {
				kept = append(kept, competition)
			}
		}
		competitions = kept
	}
	if *asJSON {
		return printJSON(competitions)
	}
	if len(competitions) == 0 {
		fmt.Println("no competitions")
		return nil
	}

	table := newTable()
	fmt.Fprintln(table, "ID\tGAME\tSTATE\tHANDS\tMATCH\tPLAYERS")
	for _, competition := range competitions {
		names := make([]string, 0, len(competition.Players))
		for _, player := range competition.Players {
			names = append(names, player.Name)
		}
		matchID := "—"
		if competition.MatchID != nil {
			matchID = fmt.Sprint(*competition.MatchID)
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%d\t%s\t%s\n",
			shortDigest(competition.ID), competition.Config.Game,
			competitionState(competition), competition.Config.Hands, matchID,
			strings.Join(dedupePlayers(names), " vs "))
	}
	return table.Flush()
}

func competeCommand() command {
	return command{
		name:    "compete",
		summary: "queue a competition between version ids",
		usage:   "arena compete --game GAME --versions ID,ID[,…] [--hands N] [--seeded] [--cores N] [--timeout-ms N]",
		needKey: true,
		run:     runCompete,
	}
}

func runCompete(ctx context.Context, client *arena.Client, args []string) error {
	flags := flag.NewFlagSet("compete", flag.ContinueOnError)
	game := flags.String("game", "27td-fl", "game id")
	versions := flags.String("versions", "", "comma-separated version ids, one per seat")
	hands := flags.Int("hands", 10000, "hands to play (1..300000)")
	seeded := flags.Bool("seeded", false, "use seeded dealing instead of duplicate")
	cores := flags.Int("cores", 3, "CPU cores (1..8)")
	timeoutMs := flags.Int("timeout-ms", 5000, "per-decision deadline in ms (1..5000)")
	watch := flags.Bool("watch", false, "follow progress until the match ends")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if *versions == "" {
		return fmt.Errorf("usage: %s", competeCommand().usage)
	}

	config := arena.CompetitionConfig{
		Game:              *game,
		Players:           splitCSV(*versions),
		Hands:             *hands,
		Duplicate:         !*seeded,
		CPUCores:          *cores,
		DecisionTimeoutMs: *timeoutMs,
	}
	// Validate before spending a request — the platform's own UI does the same.
	if err := config.Validate(arena.IsOFC(config.Game)); err != nil {
		return err
	}

	created, err := client.CreateCompetition(ctx, config)
	if err != nil {
		return err
	}
	fmt.Printf("queued %s — %s, %d seats, %d hands\n",
		created.ID, config.Game, len(config.Players), config.Hands)
	for _, player := range created.Players {
		fmt.Printf("  seat %d  %s (%s)\n", player.Player, player.Name, shortDigest(player.ArtifactDigest))
	}
	fmt.Println("\nevery competition ends at its hand count or 10 minutes, whichever comes first")

	if *watch {
		return watchCompetition(ctx, client, created.ID)
	}
	fmt.Printf("\nfollow with: arena watch %s\n", created.ID)
	return nil
}

func watchCommand() command {
	return command{
		name:    "watch",
		summary: "follow a competition until it finishes",
		usage:   "arena watch <competitionId>",
		needKey: true,
		run: func(ctx context.Context, client *arena.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: arena watch <competitionId>")
			}
			return watchCompetition(ctx, client, args[0])
		},
	}
}

// watchCompetition polls until the competition reaches a terminal state.
//
// The cadence comes from /api/health (liveTransport "poll", liveIntervalMs
// 5000); once a match exists, /api/progress gives finer-grained hand counts.
func watchCompetition(ctx context.Context, client *arena.Client, id string) error {
	const interval = 5 * time.Second
	var revision int64

	for {
		competition, err := client.Competition(ctx, id)
		if err != nil {
			return err
		}

		line := fmt.Sprintf("%-14s", competitionState(*competition))
		if competition.MatchID != nil {
			progress, err := client.Progress(ctx, *competition.MatchID, revision)
			if err != nil {
				return err
			}
			if progress != nil {
				revision = progress.Revision
				for _, match := range progress.Matches {
					if match.ID == *competition.MatchID {
						line += fmt.Sprintf("hand %d/%d", match.CompletedHands, competition.Config.Hands)
					}
				}
			}
		}
		fmt.Printf("\r%-60s", line)

		if competition.Done() {
			fmt.Println()
			if competition.MatchID != nil {
				fmt.Printf("finished — arena match %d\n", *competition.MatchID)
			} else {
				fmt.Printf("finished — %s\n", competitionState(*competition))
			}
			return nil
		}

		select {
		case <-ctx.Done():
			fmt.Println()
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

func competitionState(competition arena.Competition) string {
	if competition.FailureCode != nil && *competition.FailureCode != "" {
		return competition.State + " (" + *competition.FailureCode + ")"
	}
	return competition.State
}
