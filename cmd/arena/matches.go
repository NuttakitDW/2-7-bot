package main

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/nuttakit/2-7-bot/internal/arena"
)

func matchesCommand() command {
	return command{
		name:    "matches",
		summary: "list recent matches",
		usage:   "arena matches [--limit N] [--game GAME] [--json]",
		needKey: true,
		run:     runMatches,
	}
}

func runMatches(ctx context.Context, client *arena.Client, args []string) error {
	flags := flag.NewFlagSet("matches", flag.ContinueOnError)
	limit := flags.Int("limit", 20, "how many matches to fetch")
	game := flags.String("game", "", "only matches of this game")
	asJSON := flags.Bool("json", false, "print raw JSON")
	if err := parseFlags(flags, args); err != nil {
		return err
	}

	matches, err := client.ListMatches(ctx, *limit)
	if err != nil {
		return err
	}
	if *game != "" {
		kept := matches[:0]
		for _, match := range matches {
			if match.Game == *game {
				kept = append(kept, match)
			}
		}
		matches = kept
	}
	if *asJSON {
		return printJSON(matches)
	}
	if len(matches) == 0 {
		fmt.Println("no matches")
		return nil
	}

	table := newTable()
	fmt.Fprintln(table, "ID\tGAME\tSTATUS\tHANDS\tDEAL\tPLAYERS\tRESULT")
	for _, match := range matches {
		fmt.Fprintf(table, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			match.ID, match.Game, matchStatus(match),
			fmt.Sprintf("%d/%d", match.CompletedHands, match.ConfiguredHands),
			match.DealMode,
			strings.Join(dedupePlayers(match.Players), " vs "),
			formatRates(match))
	}
	return table.Flush()
}

func matchCommand() command {
	return command{
		name:    "match",
		summary: "one match with per-player statistics",
		usage:   "arena match <id> [--json]",
		needKey: true,
		run:     runMatch,
	}
}

func runMatch(ctx context.Context, client *arena.Client, args []string) error {
	flags := flag.NewFlagSet("match", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "print raw JSON")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() < 1 {
		return fmt.Errorf("usage: arena match <id>")
	}
	id, err := strconv.Atoi(flags.Arg(0))
	if err != nil {
		return fmt.Errorf("match id must be a number: %w", err)
	}

	detail, err := client.Match(ctx, id)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(detail)
	}

	info := detail.MatchInfo
	fmt.Printf("match %d — %s (%s, %s dealing)\n", info.ID, info.Game, matchStatus(info), info.DealMode)
	fmt.Printf("hands %d/%d · %d CPU · %dms decision limit\n",
		info.CompletedHands, info.ConfiguredHands, info.CPUCores, info.DecisionTimeoutMs)
	fmt.Printf("started %s\n\n", formatMillis(info.StartedAtMs))

	table := newTable()
	fmt.Fprintf(table, "SEAT\tNAME\tHANDS\tFAULTS\t%s\tCI95\tVPIP\tPFR\tAF\tWTSD\tW$SD\tFOLD\tMEAN\tMAX\n", info.RateUnit)
	for _, stats := range detail.Stats {
		rate, ci := "—", "—"
		if stats.Player < len(info.RatePer100Milli) {
			rate = fmt.Sprintf("%+.2f", arena.Milli(info.RatePer100Milli[stats.Player]))
		}
		if stats.Player < len(info.Confidence95Milli) && info.Confidence95Milli[stats.Player] != nil {
			ci = fmt.Sprintf("±%.2f", arena.Milli(*info.Confidence95Milli[stats.Player]))
		}
		aggression := "—"
		if stats.AggressionMilli != nil {
			aggression = fmt.Sprintf("%.2f", arena.Milli(*stats.AggressionMilli))
		}
		fmt.Fprintf(table, "%d\t%s\t%d\t%s\t%s\t%s\t%.1f%%\t%.1f%%\t%s\t%.1f%%\t%.1f%%\t%.1f%%\t%.2fms\t%.2fms\n",
			stats.Player, stats.Name, stats.Hands, faultMarker(stats.Faults), rate, ci,
			arena.Percent(stats.VpipPpm), arena.Percent(stats.PfrPpm), aggression,
			arena.Percent(stats.WentToShowdownPpm), arena.Percent(stats.WonAtShowdownPpm),
			arena.Percent(stats.FoldRatePpm),
			arena.Millis(stats.Decisions.MeanMicros), arena.Millis(stats.Decisions.MaxMicros))
	}
	if err := table.Flush(); err != nil {
		return err
	}

	fmt.Printf("\nhand logs: %d sampled, %d biggest\n", detail.SampleHandCount, detail.BiggestHandCount)
	return nil
}

// matchStatus folds terminalReason into the status, since a match that ran out
// of wall clock reports "completed" with a reason rather than a failure.
func matchStatus(match arena.MatchSummary) string {
	if match.TerminalReason != nil && *match.TerminalReason != "" {
		return match.Status + " (" + *match.TerminalReason + ")"
	}
	return match.Status
}

func faultMarker(faults int) string {
	if faults > 0 {
		return fmt.Sprintf("%d !", faults)
	}
	return "0"
}

// dedupePlayers collapses a repeated bot name into "name x N", which duplicate
// six-handed matches produce constantly.
func dedupePlayers(players []string) []string {
	out := make([]string, 0, len(players))
	for i := 0; i < len(players); {
		j := i
		for j < len(players) && players[j] == players[i] {
			j++
		}
		if j-i > 1 {
			out = append(out, fmt.Sprintf("%s x%d", players[i], j-i))
		} else {
			out = append(out, players[i])
		}
		i = j
	}
	return out
}

func formatRates(match arena.MatchSummary) string {
	if len(match.RatePer100Milli) == 0 {
		return "—"
	}
	parts := make([]string, 0, len(match.RatePer100Milli))
	for _, rate := range match.RatePer100Milli {
		parts = append(parts, fmt.Sprintf("%+.1f", arena.Milli(rate)))
	}
	return strings.Join(parts, "/") + " " + match.RateUnit
}
