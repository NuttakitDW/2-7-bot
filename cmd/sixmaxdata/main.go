// Command sixmaxdata collects unbiased hosted six-player hand samples.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/nuttakit/2-7-bot/internal/arena"
	"github.com/nuttakit/2-7-bot/internal/sixmaxdata"
)

func main() {
	output := flag.String("out", "bin/sixmax/data", "output directory")
	workers := flag.Int("concurrency", 4, "concurrent hand reads (maximum 4)")
	retries := flag.Int("retries", 2, "retries after a failed read (maximum 4)")
	baseURL := flag.String("base-url", arena.DefaultBaseURL, "arena base URL")
	flag.Parse()
	ids, err := parseIDs(flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	apiKey, err := arena.ResolveAPIKey(".env")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	client := arena.New(*baseURL, apiKey)
	for _, id := range ids {
		collected, fetchErr := sixmaxdata.FetchMatch(ctx, client, id, *workers, *retries)
		if fetchErr != nil {
			fmt.Fprintln(os.Stderr, fetchErr)
			os.Exit(1)
		}
		hands, observations, storeErr := sixmaxdata.Store(*output, collected)
		if storeErr != nil {
			fmt.Fprintln(os.Stderr, storeErr)
			os.Exit(1)
		}
		fmt.Printf("match %d: stored %d sampled hands and %d observations\n", id, hands, observations)
	}
}

func parseIDs(args []string) ([]int, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: sixmaxdata [flags] MATCH_ID [MATCH_ID ...]")
	}
	var ids []int
	seen := map[int]bool{}
	for _, arg := range args {
		for _, field := range strings.Split(arg, ",") {
			id, err := strconv.Atoi(strings.TrimSpace(field))
			if err != nil || id <= 0 {
				return nil, fmt.Errorf("invalid match ID %q", field)
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids, nil
}
