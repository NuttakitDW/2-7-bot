package arena

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The platform uses two different error envelopes depending on the endpoint.
// A client that understands only one reports "HTTP 401" with no reason.
func TestDecodeAPIErrorEnvelopes(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantMessage string
		wantCode    string
	}{
		{
			name:        "code and message envelope",
			status:      401,
			body:        `{"code":"authentication_required","message":"valid login credentials or an API key are required"}`,
			wantMessage: "valid login credentials or an API key are required",
			wantCode:    "authentication_required",
		},
		{
			name:        "error envelope",
			status:      400,
			body:        `{"error":"unknown hand collection"}`,
			wantMessage: "unknown hand collection",
		},
		{
			name:        "error wins over message, matching the platform's own client",
			status:      400,
			body:        `{"error":"specific","message":"generic"}`,
			wantMessage: "specific",
		},
		{
			name:        "html body is not json",
			status:      502,
			body:        "<html>bad gateway</html>",
			wantMessage: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			apiErr := decodeAPIError(test.status, []byte(test.body))
			if apiErr.Status != test.status {
				t.Errorf("status = %d, want %d", apiErr.Status, test.status)
			}
			if apiErr.Message != test.wantMessage {
				t.Errorf("message = %q, want %q", apiErr.Message, test.wantMessage)
			}
			if apiErr.Code != test.wantCode {
				t.Errorf("code = %q, want %q", apiErr.Code, test.wantCode)
			}
			if apiErr.Error() == "" {
				t.Error("Error() must never be empty")
			}
		})
	}
}

func TestClientSendsBearerTokenAndSurfacesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"authentication_required","message":"nope"}`))
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	_, err := client.Health(t.Context())
	if err == nil {
		t.Fatal("expected an error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *APIError: %v", err)
	}
	if apiErr.Status != http.StatusUnauthorized || !strings.Contains(apiErr.Error(), "nope") {
		t.Errorf("unexpected error: %v", apiErr)
	}
}

// /api/progress answers 204 when nothing changed. That is a success, and a
// client that treats it as an error would break every watch loop.
func TestNoContentIsNotAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	var out map[string]any
	if err := New(server.URL, "k").get(t.Context(), "/api/progress", &out); err != nil {
		t.Fatalf("204 reported as error: %v", err)
	}
	if out != nil {
		t.Errorf("204 must leave the destination untouched, got %v", out)
	}
}
