package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNotConfigured verifies the graceful-degradation contract: no key ⇒ not
// configured ⇒ Generate returns ErrNotConfigured (never panics, never calls out).
func TestNotConfigured(t *testing.T) {
	c := NewClient("", "", "")
	if c.Configured() {
		t.Fatal("client with empty key should report not configured")
	}
	if _, err := c.Generate(context.Background(), "sys", "user", 100); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}

// TestDefaults verifies an empty base URL/model fall back to the OSS defaults.
func TestDefaults(t *testing.T) {
	c := NewClient("k", "", "")
	if c.Model() == "" {
		t.Fatal("expected a default open-weight model id")
	}
	if c.baseURL != defaultBaseURL {
		t.Fatalf("expected default base URL, got %s", c.baseURL)
	}
}

// TestGenerateHappyPath verifies the OpenAI-compatible request/response wiring.
func TestGenerateHappyPath(t *testing.T) {
	var gotAuth, gotPath string
	var gotReq chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		resp := chatResponse{Model: "test-model"}
		resp.Choices = append(resp.Choices, struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}{})
		resp.Choices[0].Message.Content = "  Dispatch looks clear.  "
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient("secret", srv.URL, "my-model")
	out, err := c.Generate(context.Background(), "system text", "user text", 256)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "Dispatch looks clear." {
		t.Fatalf("content not trimmed/returned: %q", out)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("expected Bearer auth, got %q", gotAuth)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("unexpected path %q", gotPath)
	}
	if gotReq.Model != "my-model" || len(gotReq.Messages) != 2 {
		t.Errorf("unexpected request shape: %+v", gotReq)
	}
}
