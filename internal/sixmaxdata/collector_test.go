package sixmaxdata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nuttakit/2-7-bot/internal/arena"
)

type fakeSource struct {
	mu                    sync.Mutex
	inFlight, maxInFlight int
	failures              map[int]int
	calls                 map[int]int
}

func (f *fakeSource) Match(context.Context, int) (*arena.MatchDetail, error) {
	return &arena.MatchDetail{MatchInfo: arena.MatchSummary{ID: 41, Game: "27td-fl", DealMode: "duplicate", Players: make([]string, 6)}}, nil
}
func (f *fakeSource) Hands(context.Context, int, string, int) (*arena.HandPage, error) {
	return &arena.HandPage{Page: 0, PageCount: 1, TotalHands: 6, Hands: []arena.HandSummary{{Number: 1}, {Number: 2}, {Number: 3}, {Number: 4}, {Number: 5}, {Number: 6}}}, nil
}
func (f *fakeSource) Hand(_ context.Context, _, n int) (*arena.HandDetail, error) {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[int]int{}
	}
	f.calls[n]++
	f.inFlight++
	if f.inFlight > f.maxInFlight {
		f.maxInFlight = f.inFlight
	}
	if f.failures[n] > 0 {
		f.failures[n]--
		f.inFlight--
		f.mu.Unlock()
		return nil, errors.New("temporary")
	}
	time.Sleep(time.Millisecond)
	f.inFlight--
	f.mu.Unlock()
	return &arena.HandDetail{Hand: arena.HandSummary{Number: n}}, nil
}

func TestFetchMatchRetriesAndBoundsConcurrency(t *testing.T) {
	source := &fakeSource{failures: map[int]int{2: 1}}
	got, err := FetchMatch(context.Background(), source, 41, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Hands) != 6 {
		t.Fatalf("hands = %d", len(got.Hands))
	}
	if source.maxInFlight > 3 {
		t.Fatalf("concurrency = %d, want <= 3", source.maxInFlight)
	}
}

func TestFetchMatchRejectsNonSixMax(t *testing.T) {
	_, err := validateMatch(&arena.MatchDetail{MatchInfo: arena.MatchSummary{Game: "27td-fl", Players: []string{"a", "b"}}})
	if err == nil {
		t.Fatal("expected six-player validation error")
	}
}

func TestStoreWritesRawAndDerivedData(t *testing.T) {
	text := func(s string) *string { return &s }
	player := func(p int) *int { return &p }
	order := []int{5, 3, 2, 4, 1, 0}
	hand := &arena.HandDetail{Hand: arena.HandSummary{Number: 1, Roles: make([]string, 6)}}
	deals := []string{"2c3d4h7sKc", "2d3h5s8cQd", "2h4d6s9cKh", "3c5d7hTsAc", "4c6d8hJsAd", "5c7d9hQsAh"}
	for i, p := range order {
		hand.Events = append(hand.Events, arena.HandEvent{Kind: "initial-cards", Player: player(p), Street: text("predraw"), Cards: text(deals[i])})
	}
	hand.Events = append(hand.Events, arena.HandEvent{Kind: "action", Player: player(2), Street: text("predraw"), Action: text("fold")})
	collected := &CollectedMatch{
		Match: &arena.MatchDetail{MatchInfo: arena.MatchSummary{ID: 41, Game: "27td-fl", DealMode: "duplicate", Players: make([]string, 6)}},
		Pages: []*arena.HandPage{{Page: 0, Hands: []arena.HandSummary{hand.Hand}}},
		Hands: []*arena.HandDetail{hand},
	}
	dir := t.TempDir()
	hands, observations, err := Store(dir, collected)
	if err != nil {
		t.Fatal(err)
	}
	if hands != 1 || observations != 1 {
		t.Fatalf("stored (%d, %d), want (1, 1)", hands, observations)
	}
	for _, path := range []string{"match-41/match.json", "match-41/samples-page-0.json", "match-41/hands/1.json", "match-41/derived.json"} {
		if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
			t.Errorf("missing %s: %v", path, err)
		}
	}
}

func TestFetchMatchReturnsExhaustedRetry(t *testing.T) {
	source := &fakeSource{failures: map[int]int{1: 4}}
	if _, err := FetchMatch(context.Background(), source, 41, 9, 1); err == nil {
		t.Fatal("expected hand fetch error")
	}
}

func TestFetchMatchCapsRetries(t *testing.T) {
	source := &fakeSource{failures: map[int]int{1: 5}}
	if _, err := FetchMatch(context.Background(), source, 41, 1, 100); err == nil {
		t.Fatal("expected retry cap to stop the request")
	}
}

func TestFetchMatchCancelsRemainingHandsAfterError(t *testing.T) {
	source := &fakeSource{failures: map[int]int{1: 1}}
	if _, err := FetchMatch(context.Background(), source, 41, 1, 0); err == nil {
		t.Fatal("expected hand fetch error")
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.calls[2] != 0 {
		t.Fatalf("hand 2 fetched %d times after hand 1 failed", source.calls[2])
	}
}
