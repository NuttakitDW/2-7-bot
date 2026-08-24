package arena

import (
	"context"
	"fmt"
	"net/url"
)

// MatchSummary is one match's headline state. Rates are scaled integers —
// convert with Milli, Percent and Millis.
type MatchSummary struct {
	ID                int      `json:"id"`
	Game              string   `json:"game"`
	Family            string   `json:"family"`
	DealMode          string   `json:"dealMode"`
	Status            string   `json:"status"`
	ConfiguredHands   int      `json:"configuredHands"`
	CPUCores          int      `json:"cpuCores"`
	DecisionTimeoutMs int      `json:"decisionTimeoutMs"`
	CurrentHand       int      `json:"currentHand"`
	CompletedHands    int      `json:"completedHands"`
	Players           []string `json:"players"`
	RatePer100Milli   []int64  `json:"ratePer100Milli"`
	Confidence95Milli []*int64 `json:"confidence95Milli"`
	ConfidenceSamples []int    `json:"confidenceSamples"`
	RateUnit          string   `json:"rateUnit"`
	StartedAtMs       int64    `json:"startedAtMs"`
	UpdatedAtMs       int64    `json:"updatedAtMs"`
	TerminalReason    *string  `json:"terminalReason"`
}

// Decisions is the wall-clock timing profile of one player's decisions.
// Percentiles are histogram approximations; mean and max are exact.
type Decisions struct {
	Count      int   `json:"count"`
	MeanMicros int64 `json:"meanMicros"`
	P50Micros  int64 `json:"p50Micros"`
	P90Micros  int64 `json:"p90Micros"`
	P99Micros  int64 `json:"p99Micros"`
	MaxMicros  int64 `json:"maxMicros"`
}

// PlayerStats is one seat's result and behavioural profile.
//
// Faults is the correctness signal: any value above zero means an illegal or
// malformed action, or a missed deadline.
type PlayerStats struct {
	Player            int       `json:"player"`
	Name              string    `json:"name"`
	Hands             int       `json:"hands"`
	Faults            int       `json:"faults"`
	Decisions         Decisions `json:"decisions"`
	VpipPpm           int64     `json:"vpipPpm"`
	PfrPpm            int64     `json:"pfrPpm"`
	AggressionMilli   *int64    `json:"aggressionMilli"`
	WentToShowdownPpm int64     `json:"wentToShowdownPpm"`
	WonAtShowdownPpm  int64     `json:"wonAtShowdownPpm"`
	FoldRatePpm       int64     `json:"foldRatePpm"`
}

// MatchDetail wraps a summary with per-player statistics.
type MatchDetail struct {
	MatchInfo        MatchSummary  `json:"matchInfo"`
	Stats            []PlayerStats `json:"stats"`
	EventCursor      int64         `json:"eventCursor"`
	SampleHandCount  int           `json:"sampleHandCount"`
	BiggestHandCount int           `json:"biggestHandCount"`
}

// ListMatches returns recent matches, newest first.
func (c *Client) ListMatches(ctx context.Context, limit int) ([]MatchSummary, error) {
	if limit <= 0 {
		limit = 100
	}
	var matches []MatchSummary
	path := fmt.Sprintf("/api/matches?limit=%d", limit)
	if err := c.get(ctx, path, &matches); err != nil {
		return nil, err
	}
	return matches, nil
}

// Match returns one match with its per-player statistics.
func (c *Client) Match(ctx context.Context, id int) (*MatchDetail, error) {
	var detail MatchDetail
	path := fmt.Sprintf("/api/matches/%d", id)
	if err := c.get(ctx, path, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

// Hand collections. Anything else is rejected with 400.
const (
	CollectionSamples = "samples"
	CollectionBiggest = "biggest"
)

// HandSummary is one hand as the hosted log records it.
type HandSummary struct {
	Number      int        `json:"number"`
	Roles       []string   `json:"roles"`
	Status      string     `json:"status"`
	PotMilli    int64      `json:"potMilli"`
	AwardsMilli []*int64   `json:"awardsMilli"`
	NetMilli    []int64    `json:"netMilli"`
	Winner      *int       `json:"winner"`
	FinalCards  [][]string `json:"finalCards"`
	Showdown    bool       `json:"showdown"`
}

// HandPage is one page of a hand collection.
type HandPage struct {
	Collection string        `json:"collection"`
	Page       int           `json:"page"`
	PageCount  int           `json:"pageCount"`
	TotalHands int           `json:"totalHands"`
	Hands      []HandSummary `json:"hands"`
}

// HandEvent is one entry of the hosted hand log.
//
// This is the PLATFORM's vocabulary, not the upstream wire Event union — cards
// arrive as a packed string such as "JcKh4hJd". Do not conflate the two; see
// docs/arena/http-api.md.
type HandEvent struct {
	ID          int64   `json:"id"`
	HandNumber  int     `json:"handNumber"`
	Kind        string  `json:"kind"`
	Player      *int    `json:"player"`
	Street      *string `json:"street"`
	Action      *string `json:"action"`
	Cards       *string `json:"cards"`
	AmountMilli *int64  `json:"amountMilli"`
	Detail      *string `json:"detail"`
}

// HandDetail is one hand plus its event list.
type HandDetail struct {
	Hand   HandSummary `json:"hand"`
	Events []HandEvent `json:"events"`
}

// Hands returns one page of a match's hand collection.
func (c *Client) Hands(ctx context.Context, matchID int, collection string, page int) (*HandPage, error) {
	if collection == "" {
		collection = CollectionSamples
	}
	if collection != CollectionSamples && collection != CollectionBiggest {
		return nil, fmt.Errorf("unknown hand collection %q (want %q or %q)",
			collection, CollectionSamples, CollectionBiggest)
	}
	var handPage HandPage
	path := fmt.Sprintf("/api/matches/%d/hands?collection=%s&page=%d",
		matchID, url.QueryEscape(collection), page)
	if err := c.get(ctx, path, &handPage); err != nil {
		return nil, err
	}
	return &handPage, nil
}

// Hand returns one hand and its events.
func (c *Client) Hand(ctx context.Context, matchID, handNumber int) (*HandDetail, error) {
	var detail HandDetail
	path := fmt.Sprintf("/api/matches/%d/hands/%d", matchID, handNumber)
	if err := c.get(ctx, path, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}
