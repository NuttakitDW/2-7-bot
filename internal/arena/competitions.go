package arena

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// Competition limits, as the platform's own client enforces them before
// submitting. Checking locally turns a rejected request into a clear message.
const (
	MinSeats             = 2
	MaxSeats             = 6
	MaxHands             = 300_000
	MaxCPUCores          = 8
	MaxDecisionTimeoutMs = 5000
)

// CompetitionConfig is the requested match.
//
// Players holds VERSION ids — not bot ids — one per seat in seat order. The
// same version may appear more than once to seat a bot against itself.
type CompetitionConfig struct {
	Game              string   `json:"game"`
	Players           []string `json:"players"`
	Hands             int      `json:"hands"`
	Duplicate         bool     `json:"duplicate"`
	CPUCores          int      `json:"cpuCores"`
	DecisionTimeoutMs int      `json:"decisionTimeoutMs"`
}

// CompetitionPlayer is one seated version.
type CompetitionPlayer struct {
	Player         int    `json:"player"`
	BotID          string `json:"botId"`
	VersionID      string `json:"versionId"`
	Name           string `json:"name"`
	ArtifactDigest string `json:"artifactDigest"`
}

// Competition is a queued, running or finished match request.
type Competition struct {
	ID              string              `json:"id"`
	Config          CompetitionConfig   `json:"config"`
	State           string              `json:"state"`
	Players         []CompetitionPlayer `json:"players"`
	CreatedAtMs     int64               `json:"createdAtMs"`
	UpdatedAtMs     int64               `json:"updatedAtMs"`
	MatchID         *int                `json:"matchId"`
	FailureCode     *string             `json:"failureCode"`
	PeakMemoryBytes int64               `json:"peakMemoryBytes"`
}

// Done reports whether the competition has reached a terminal state.
func (c Competition) Done() bool {
	switch c.State {
	case "completed", "failed", "cancelled":
		return true
	}
	return false
}

// Validate applies the platform's client-side rules.
func (c CompetitionConfig) Validate(isOFC bool) error {
	if c.Game == "" {
		return fmt.Errorf("game is required")
	}
	seats := len(c.Players)
	if seats < MinSeats || seats > MaxSeats {
		return fmt.Errorf("a competition seats %d to %d players, got %d", MinSeats, MaxSeats, seats)
	}
	for i, versionID := range c.Players {
		if strings.TrimSpace(versionID) == "" {
			return fmt.Errorf("seat %d has no version id", i)
		}
	}
	if c.Hands < 1 || c.Hands > MaxHands {
		return fmt.Errorf("hands must be between 1 and %d, got %d", MaxHands, c.Hands)
	}
	if c.CPUCores < 1 || c.CPUCores > MaxCPUCores {
		return fmt.Errorf("cpuCores must be between 1 and %d, got %d", MaxCPUCores, c.CPUCores)
	}
	if c.DecisionTimeoutMs < 1 || c.DecisionTimeoutMs > MaxDecisionTimeoutMs {
		return fmt.Errorf("decisionTimeoutMs must be between 1 and %d, got %d",
			MaxDecisionTimeoutMs, c.DecisionTimeoutMs)
	}
	if c.Duplicate {
		if isOFC {
			return fmt.Errorf("duplicate dealing is not available for OFC games")
		}
		if c.Hands%seats != 0 {
			return fmt.Errorf("duplicate dealing needs hands divisible by %d seats, got %d", seats, c.Hands)
		}
	}
	return nil
}

// ListCompetitions returns every visible competition.
func (c *Client) ListCompetitions(ctx context.Context) ([]Competition, error) {
	var competitions []Competition
	if err := c.get(ctx, "/api/competitions", &competitions); err != nil {
		return nil, err
	}
	return competitions, nil
}

// Competition returns one competition.
func (c *Client) Competition(ctx context.Context, id string) (*Competition, error) {
	var competition Competition
	if err := c.get(ctx, "/api/competitions/"+url.PathEscape(id), &competition); err != nil {
		return nil, err
	}
	return &competition, nil
}

// CreateCompetition queues a match.
func (c *Client) CreateCompetition(ctx context.Context, config CompetitionConfig) (*Competition, error) {
	var created Competition
	if err := c.postJSON(ctx, "/api/competitions", config, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// ProgressMatch is one match's live position.
type ProgressMatch struct {
	ID             int    `json:"id"`
	Status         string `json:"status"`
	CurrentHand    int    `json:"currentHand"`
	CompletedHands int    `json:"completedHands"`
	UpdatedAtMs    int64  `json:"updatedAtMs"`
	EventCursor    int64  `json:"eventCursor"`
}

// Progress is a live-standings snapshot.
type Progress struct {
	Revision int64           `json:"revision"`
	Matches  []ProgressMatch `json:"matches"`
}

// Progress polls live match state. A nil result with a nil error means the
// server answered 204 — nothing has changed since the given revision.
func (c *Client) Progress(ctx context.Context, matchID int, since int64) (*Progress, error) {
	query := url.Values{}
	if matchID > 0 {
		query.Set("matchId", fmt.Sprint(matchID))
	}
	if since > 0 {
		query.Set("since", fmt.Sprint(since))
	}
	path := "/api/progress"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}

	var progress Progress
	if err := c.get(ctx, path, &progress); err != nil {
		return nil, err
	}
	if progress.Revision == 0 && progress.Matches == nil {
		return nil, nil // 204: nothing new
	}
	return &progress, nil
}

// IsOFC reports whether a game id belongs to the Open Face Chinese family,
// which speaks a different protocol and cannot use duplicate dealing.
func IsOFC(game string) bool {
	return game == "ofc" || strings.HasPrefix(game, "ofc-")
}
