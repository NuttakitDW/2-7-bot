package sixmaxdata

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/nuttakit/2-7-bot/internal/arena"
)

type Source interface {
	Match(context.Context, int) (*arena.MatchDetail, error)
	Hands(context.Context, int, string, int) (*arena.HandPage, error)
	Hand(context.Context, int, int) (*arena.HandDetail, error)
}

type CollectedMatch struct {
	Match *arena.MatchDetail
	Pages []*arena.HandPage
	Hands []*arena.HandDetail
}

const (
	maxConcurrency = 4
	maxRetries     = 4
)

func validateMatch(detail *arena.MatchDetail) (*arena.MatchDetail, error) {
	if detail == nil {
		return nil, fmt.Errorf("empty match response")
	}
	if detail.MatchInfo.Game != "27td-fl" {
		return nil, fmt.Errorf("match %d game is %q, want 27td-fl", detail.MatchInfo.ID, detail.MatchInfo.Game)
	}
	if len(detail.MatchInfo.Players) != 6 {
		return nil, fmt.Errorf("match %d has %d players, want 6", detail.MatchInfo.ID, len(detail.MatchInfo.Players))
	}
	return detail, nil
}

// FetchMatch downloads the unbiased samples collection and each referenced
// hand. concurrency is clamped to four and retries counts additional attempts.
func FetchMatch(ctx context.Context, source Source, matchID, concurrency, retries int) (*CollectedMatch, error) {
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > maxConcurrency {
		concurrency = maxConcurrency
	}
	if retries < 0 {
		retries = 0
	}
	if retries > maxRetries {
		retries = maxRetries
	}
	detail, err := retry(ctx, retries, func() (*arena.MatchDetail, error) { return source.Match(ctx, matchID) })
	if err != nil {
		return nil, fmt.Errorf("match %d: %w", matchID, err)
	}
	if _, err := validateMatch(detail); err != nil {
		return nil, err
	}

	first, err := retry(ctx, retries, func() (*arena.HandPage, error) { return source.Hands(ctx, matchID, arena.CollectionSamples, 0) })
	if err != nil {
		return nil, fmt.Errorf("match %d samples page 0: %w", matchID, err)
	}
	pages := []*arena.HandPage{first}
	for page := 1; page < first.PageCount; page++ {
		page := page
		got, pageErr := retry(ctx, retries, func() (*arena.HandPage, error) { return source.Hands(ctx, matchID, arena.CollectionSamples, page) })
		if pageErr != nil {
			return nil, fmt.Errorf("match %d samples page %d: %w", matchID, page, pageErr)
		}
		pages = append(pages, got)
	}
	numbers := make([]int, 0, first.TotalHands)
	seen := make(map[int]bool)
	for _, page := range pages {
		for _, hand := range page.Hands {
			if !seen[hand.Number] {
				seen[hand.Number] = true
				numbers = append(numbers, hand.Number)
			}
		}
	}
	sort.Ints(numbers)

	type result struct {
		detail *arena.HandDetail
		err    error
	}
	fetchCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	jobs := make(chan int)
	results := make(chan result, len(numbers))
	var workers sync.WaitGroup
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				if fetchCtx.Err() != nil {
					return
				}
				var number int
				var ok bool
				select {
				case <-fetchCtx.Done():
					return
				case number, ok = <-jobs:
					if !ok {
						return
					}
				}
				hand, fetchErr := retry(fetchCtx, retries, func() (*arena.HandDetail, error) { return source.Hand(fetchCtx, matchID, number) })
				if fetchErr != nil {
					fetchErr = fmt.Errorf("match %d hand %d: %w", matchID, number, fetchErr)
					cancel(fetchErr)
				}
				results <- result{detail: hand, err: fetchErr}
				if fetchErr != nil {
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, number := range numbers {
			select {
			case jobs <- number:
			case <-fetchCtx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()
	hands := make([]*arena.HandDetail, 0, len(numbers))
	var firstErr error
	for got := range results {
		if got.err != nil {
			if firstErr == nil {
				firstErr = got.err
			}
			continue
		}
		hands = append(hands, got.detail)
	}
	if firstErr != nil {
		if cause := context.Cause(fetchCtx); cause != nil && ctx.Err() == nil {
			return nil, cause
		}
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(hands, func(i, j int) bool { return hands[i].Hand.Number < hands[j].Hand.Number })
	return &CollectedMatch{Match: detail, Pages: pages, Hands: hands}, nil
}

func retry[T any](ctx context.Context, retries int, call func() (T, error)) (T, error) {
	var zero T
	var err error
	for attempt := 0; attempt <= retries; attempt++ {
		var value T
		value, err = call()
		if err == nil {
			return value, nil
		}
		if attempt == retries {
			break
		}
		delay := time.Duration(50*(1<<attempt)) * time.Millisecond
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return zero, ctx.Err()
		}
	}
	return zero, err
}

// Store writes fetched source responses and derived observations beneath a
// match-specific directory. Callers should place root under ignored build data.
func Store(root string, collected *CollectedMatch) (int, int, error) {
	if collected == nil || collected.Match == nil {
		return 0, 0, fmt.Errorf("nothing collected")
	}
	matchID := collected.Match.MatchInfo.ID
	dir := filepath.Join(root, fmt.Sprintf("match-%d", matchID))
	if err := os.MkdirAll(filepath.Join(dir, "hands"), 0o755); err != nil {
		return 0, 0, err
	}
	if err := writeJSON(filepath.Join(dir, "match.json"), collected.Match); err != nil {
		return 0, 0, err
	}
	for _, page := range collected.Pages {
		if err := writeJSON(filepath.Join(dir, fmt.Sprintf("samples-page-%d.json", page.Page)), page); err != nil {
			return 0, 0, err
		}
	}
	records := make([]HandRecord, 0, len(collected.Hands))
	observations := 0
	for _, hand := range collected.Hands {
		if err := writeJSON(filepath.Join(dir, "hands", fmt.Sprintf("%d.json", hand.Hand.Number)), hand); err != nil {
			return len(records), observations, err
		}
		record, err := ParseHand(matchID, collected.Match.MatchInfo.DealMode, *hand)
		if err != nil {
			return len(records), observations, fmt.Errorf("match %d hand %d: %w", matchID, hand.Hand.Number, err)
		}
		records = append(records, record)
		observations += len(record.Observations)
	}
	if err := writeJSON(filepath.Join(dir, "derived.json"), records); err != nil {
		return len(records), observations, err
	}
	return len(records), observations, nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
