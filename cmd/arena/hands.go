package main

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/nuttakit/2-7-bot/internal/arena"
)

func handsCommand() command {
	return command{
		name:    "hands",
		summary: "list a match's sampled or biggest hands",
		usage:   "arena hands <matchId> [--collection samples|biggest] [--page N] [--json]",
		needKey: true,
		run:     runHands,
	}
}

func runHands(ctx context.Context, client *arena.Client, args []string) error {
	flags := flag.NewFlagSet("hands", flag.ContinueOnError)
	collection := flags.String("collection", arena.CollectionSamples, "samples or biggest")
	page := flags.Int("page", 0, "zero-based page")
	asJSON := flags.Bool("json", false, "print raw JSON")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() < 1 {
		return fmt.Errorf("usage: arena hands <matchId>")
	}
	matchID, err := strconv.Atoi(flags.Arg(0))
	if err != nil {
		return fmt.Errorf("match id must be a number: %w", err)
	}

	handPage, err := client.Hands(ctx, matchID, *collection, *page)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(handPage)
	}

	fmt.Printf("%s — page %d of %d, %d hands total\n\n",
		handPage.Collection, handPage.Page+1, handPage.PageCount, handPage.TotalHands)

	table := newTable()
	fmt.Fprintln(table, "HAND\tPOT\tWINNER\tSHOWDOWN\tNET\tFINAL CARDS")
	for _, hand := range handPage.Hands {
		winner := "—"
		if hand.Winner != nil {
			winner = fmt.Sprintf("seat %d", *hand.Winner)
		}
		nets := make([]string, len(hand.NetMilli))
		for i, net := range hand.NetMilli {
			nets[i] = fmt.Sprintf("%+.1f", arena.Milli(net))
		}
		cards := make([]string, len(hand.FinalCards))
		for i, seat := range hand.FinalCards {
			cards[i] = strings.Join(seat, "")
		}
		fmt.Fprintf(table, "%d\t%.1f\t%s\t%t\t%s\t%s\n",
			hand.Number, arena.Milli(hand.PotMilli), winner, hand.Showdown,
			strings.Join(nets, "/"), strings.Join(cards, " | "))
	}
	return table.Flush()
}

func handCommand() command {
	return command{
		name:    "hand",
		summary: "one hand with its full platform event log",
		usage:   "arena hand <matchId> <handNumber> [--json]",
		needKey: true,
		run:     runHand,
	}
}

func runHand(ctx context.Context, client *arena.Client, args []string) error {
	flags := flag.NewFlagSet("hand", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "print raw JSON")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() < 2 {
		return fmt.Errorf("usage: arena hand <matchId> <handNumber>")
	}
	matchID, err := strconv.Atoi(flags.Arg(0))
	if err != nil {
		return fmt.Errorf("match id must be a number: %w", err)
	}
	handNumber, err := strconv.Atoi(flags.Arg(1))
	if err != nil {
		return fmt.Errorf("hand number must be a number: %w", err)
	}

	detail, err := client.Hand(ctx, matchID, handNumber)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(detail)
	}

	hand := detail.Hand
	winner := "—"
	if hand.Winner != nil {
		winner = fmt.Sprintf("seat %d", *hand.Winner)
	}
	fmt.Printf("hand %d — pot %.1f, winner %s, showdown %t\n",
		hand.Number, arena.Milli(hand.PotMilli), winner, hand.Showdown)
	fmt.Printf("roles: %s\n\n", strings.Join(hand.Roles, ", "))

	table := newTable()
	fmt.Fprintln(table, "STREET\tSEAT\tKIND\tACTION\tCARDS\tAMOUNT")
	for _, event := range detail.Events {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n",
			derefString(event.Street), derefInt(event.Player), event.Kind,
			derefString(event.Action), derefString(event.Cards),
			formatAmount(event.AmountMilli))
	}
	return table.Flush()
}

func derefString(value *string) string {
	if value == nil || *value == "" {
		return "—"
	}
	return *value
}

func derefInt(value *int) string {
	if value == nil {
		return "—"
	}
	return strconv.Itoa(*value)
}

func formatAmount(milli *int64) string {
	if milli == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f", arena.Milli(*milli))
}
