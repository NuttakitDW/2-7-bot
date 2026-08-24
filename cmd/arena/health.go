package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/nuttakit/2-7-bot/internal/arena"
)

func healthCommand() command {
	return command{
		name:    "health",
		summary: "service status and the account behind the API key",
		usage:   "arena health [--json]",
		needKey: true,
		run:     runHealth,
	}
}

func runHealth(ctx context.Context, client *arena.Client, args []string) error {
	flags := flag.NewFlagSet("health", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "print raw JSON")
	if err := parseFlags(flags, args); err != nil {
		return err
	}

	health, err := client.Health(ctx)
	if err != nil {
		return err
	}
	account, err := client.Me(ctx)
	if err != nil {
		return err
	}

	if *asJSON {
		return printJSON(struct {
			Health  *arena.Health  `json:"health"`
			Account *arena.Account `json:"account"`
		}{health, account})
	}

	fmt.Printf("status    %s (%s, data %s)\n", health.Status, health.Mode, health.Data)
	fmt.Printf("live      %s every %dms\n", health.LiveTransport, health.LiveIntervalMs)
	fmt.Printf("account   %s (%s)\n", account.Username, account.Role)
	return nil
}
