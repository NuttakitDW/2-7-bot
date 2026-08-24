package main

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/nuttakit/2-7-bot/internal/arena"
)

func botsCommand() command {
	return command{
		name:    "bots",
		summary: "list bots and their latest version's declared capabilities",
		usage:   "arena bots [--all] [--game GAME] [--json]",
		needKey: true,
		run:     runBots,
	}
}

func runBots(ctx context.Context, client *arena.Client, args []string) error {
	flags := flag.NewFlagSet("bots", flag.ContinueOnError)
	all := flags.Bool("all", false, "include bots owned by other accounts and system baselines")
	game := flags.String("game", "", "only bots whose latest version declares this game")
	asJSON := flags.Bool("json", false, "print raw JSON")
	if err := parseFlags(flags, args); err != nil {
		return err
	}

	var (
		bots []arena.Bot
		err  error
	)
	if *all {
		bots, err = client.ListBots(ctx)
	} else {
		bots, err = client.ListOwnedBots(ctx)
	}
	if err != nil {
		return err
	}

	if *game != "" {
		bots = filterByGame(bots, *game)
	}
	if *asJSON {
		return printJSON(bots)
	}
	if len(bots) == 0 {
		fmt.Println("no bots")
		return nil
	}

	table := newTable()
	fmt.Fprintln(table, "NAME\tSTATE\tGAMES\tSEATS\tSIZE\tDIGEST\tBOT ID")
	for _, bot := range bots {
		name := bot.Name
		if bot.System {
			name += " *"
		}
		games, seats, size, digest := "—", "—", "—", "—"
		if version := bot.LatestVersion; version != nil {
			games = strings.Join(version.SupportedGames, ",")
			seats = joinInts(version.SupportedPlayerCounts)
			size = humanBytes(version.ArtifactSize)
			digest = shortDigest(version.ArtifactDigest)
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			name, bot.State, games, seats, size, digest, bot.ID)
	}
	if err := table.Flush(); err != nil {
		return err
	}
	if *all {
		fmt.Println("\n* system baseline")
	}
	return nil
}

func versionsCommand() command {
	return command{
		name:    "versions",
		summary: "list a bot's immutable version history",
		usage:   "arena versions <botId> [--json]",
		needKey: true,
		run:     runVersions,
	}
}

func runVersions(ctx context.Context, client *arena.Client, args []string) error {
	flags := flag.NewFlagSet("versions", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "print raw JSON")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() < 1 {
		return fmt.Errorf("usage: arena versions <botId>")
	}

	versions, err := client.ListVersions(ctx, flags.Arg(0))
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(versions)
	}
	if len(versions) == 0 {
		fmt.Println("no versions")
		return nil
	}

	table := newTable()
	fmt.Fprintln(table, "CREATED\tGAMES\tSEATS\tSIZE\tTARGET\tDIGEST\tVERSION ID")
	for _, version := range versions {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			formatMillis(version.CreatedAtMs),
			strings.Join(version.SupportedGames, ","),
			joinInts(version.SupportedPlayerCounts),
			humanBytes(version.ArtifactSize),
			version.Target,
			shortDigest(version.ArtifactDigest),
			version.ID)
	}
	return table.Flush()
}

func filterByGame(bots []arena.Bot, game string) []arena.Bot {
	kept := make([]arena.Bot, 0, len(bots))
	for _, bot := range bots {
		if bot.LatestVersion == nil {
			continue
		}
		for _, declared := range bot.LatestVersion.SupportedGames {
			if declared == game {
				kept = append(kept, bot)
				break
			}
		}
	}
	return kept
}

func joinInts(values []int) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.Itoa(value)
	}
	return strings.Join(parts, ",")
}
