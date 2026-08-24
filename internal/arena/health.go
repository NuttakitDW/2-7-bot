package arena

import "context"

// Health is the service status document. No authentication required.
type Health struct {
	Status         string `json:"status"`
	Mode           string `json:"mode"`
	Data           string `json:"data"`
	LiveUpdates    bool   `json:"liveUpdates"`
	LiveTransport  string `json:"liveTransport"`
	LiveIntervalMs int    `json:"liveIntervalMs"`
}

// Account identifies the authenticated user.
type Account struct {
	ID                     string `json:"id"`
	Username               string `json:"username"`
	Role                   string `json:"role"`
	PasswordChangeRequired bool   `json:"passwordChangeRequired"`
	CreatedAtMs            int64  `json:"createdAtMs"`
}

// Health fetches service status.
func (c *Client) Health(ctx context.Context) (*Health, error) {
	var health Health
	if err := c.get(ctx, "/api/health", &health); err != nil {
		return nil, err
	}
	return &health, nil
}

// Me fetches the account behind the current API key.
func (c *Client) Me(ctx context.Context) (*Account, error) {
	var account Account
	if err := c.get(ctx, "/api/auth/me", &account); err != nil {
		return nil, err
	}
	return &account, nil
}
