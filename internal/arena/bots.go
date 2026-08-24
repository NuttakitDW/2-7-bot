package arena

import (
	"context"
	"fmt"
	"net/url"
)

// Bot is a named identity owning an append-only list of versions.
type Bot struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	System        bool     `json:"system"`
	State         string   `json:"state"`
	CreatedAtMs   int64    `json:"createdAtMs"`
	LatestVersion *Version `json:"latestVersion"`
}

// Version is one immutable artifact, addressed by its SHA-256 digest.
//
// Capabilities are declared at upload and frozen here: SupportedPlayerCounts
// holds exact seat counts, and they are independent — declaring 4 does not
// imply 3. See docs/arena/hosted-bot-interface.md.
type Version struct {
	ID                    string   `json:"id"`
	BotID                 string   `json:"botId"`
	ArtifactDigest        string   `json:"artifactDigest"`
	ArtifactSize          int64    `json:"artifactSize"`
	Target                string   `json:"target"`
	SupportedGames        []string `json:"supportedGames"`
	SupportedPlayerCounts []int    `json:"supportedPlayerCounts"`
	CreatedAtMs           int64    `json:"createdAtMs"`
}

// ListBots returns every visible bot, including the platform's own baselines.
func (c *Client) ListBots(ctx context.Context) ([]Bot, error) {
	return c.listBots(ctx, "/api/bots")
}

// ListOwnedBots returns only the bots belonging to this account.
func (c *Client) ListOwnedBots(ctx context.Context) ([]Bot, error) {
	return c.listBots(ctx, "/api/account/bots")
}

func (c *Client) listBots(ctx context.Context, path string) ([]Bot, error) {
	var bots []Bot
	if err := c.get(ctx, path, &bots); err != nil {
		return nil, err
	}
	return bots, nil
}

// ListVersions returns a bot's version history.
//
// Note there is no bot-detail endpoint: GET /api/bots/{id} answers 405, and
// this sub-path is the only way to read a bot's history.
func (c *Client) ListVersions(ctx context.Context, botID string) ([]Version, error) {
	if botID == "" {
		return nil, fmt.Errorf("bot id is required")
	}
	var versions []Version
	path := "/api/bots/" + url.PathEscape(botID) + "/versions"
	if err := c.get(ctx, path, &versions); err != nil {
		return nil, err
	}
	return versions, nil
}

// FindBotByName returns the owned bot with the given name, if any.
func (c *Client) FindBotByName(ctx context.Context, name string) (*Bot, error) {
	bots, err := c.ListOwnedBots(ctx)
	if err != nil {
		return nil, err
	}
	for i := range bots {
		if bots[i].Name == name {
			return &bots[i], nil
		}
	}
	return nil, nil
}
