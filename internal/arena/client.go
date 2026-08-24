// Package arena is a client for the MixedSolver Arena hosting API.
//
// The API surface it targets is documented in docs/arena/http-api.md; the
// gameplay protocol it does NOT touch is docs/protocol/WIRE_PROTOCOL.md.
package arena

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the hosted platform.
const DefaultBaseURL = "https://arena.sorawit.dev"

// Client talks to the arena's HTTP API using an API key.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// New returns a client. An empty baseURL falls back to the hosted platform.
func New(baseURL, apiKey string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

// APIError is a non-2xx response.
//
// The platform uses two different error envelopes depending on the endpoint —
// {"code","message"} and {"error"} — so both are decoded here and callers see
// one shape. See docs/arena/http-api.md.
type APIError struct {
	Status  int
	Code    string
	Message string
	Body    string
}

func (e *APIError) Error() string {
	switch {
	case e.Message != "" && e.Code != "":
		return fmt.Sprintf("arena: %s (%s, HTTP %d)", e.Message, e.Code, e.Status)
	case e.Message != "":
		return fmt.Sprintf("arena: %s (HTTP %d)", e.Message, e.Status)
	case e.Body != "":
		return fmt.Sprintf("arena: HTTP %d: %s", e.Status, truncate(e.Body, 200))
	default:
		return fmt.Sprintf("arena: HTTP %d", e.Status)
	}
}

// NotFound reports whether the request failed because the resource is absent.
func (e *APIError) NotFound() bool { return e.Status == http.StatusNotFound }

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// get issues a GET and decodes the JSON body into out.
func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, "", out)
}

// postJSON issues a POST with a JSON body and decodes the response into out.
func (c *Client) postJSON(ctx context.Context, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	return c.do(ctx, http.MethodPost, path, body, "application/json", out)
}

// do performs one request. A 204 leaves out untouched and returns nil, which
// several endpoints — notably /api/progress — use as a meaningful success.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader, contentType string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("%s %s: read body: %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return decodeAPIError(resp.StatusCode, raw)
	}
	if resp.StatusCode == http.StatusNoContent || out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", method, path, err)
	}
	return nil
}

// decodeAPIError reads whichever error envelope the endpoint happened to use.
func decodeAPIError(status int, raw []byte) *APIError {
	apiErr := &APIError{Status: status, Body: string(raw)}

	var envelope struct {
		Error   string `json:"error"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return apiErr // not JSON: Body carries whatever came back
	}

	apiErr.Code = envelope.Code
	// The platform's own client reads `error ?? message`; match that precedence.
	if envelope.Error != "" {
		apiErr.Message = envelope.Error
	} else {
		apiErr.Message = envelope.Message
	}
	return apiErr
}
