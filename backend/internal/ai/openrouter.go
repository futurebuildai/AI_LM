// Package ai is a single-key, OpenAI-compatible chat client pointed at
// OpenRouter (https://openrouter.ai/api/v1). It mirrors the proven GableLBM
// ai.Client shape: one runtime-settable API key, an open-weight default model,
// and graceful degradation — when no key is configured the client reports
// "not configured" so dependent features (e.g. the dispatch briefing) can
// degrade without hard-failing the core workflow.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://openrouter.ai/api/v1"
	// defaultModel is an open-weight (OSS) model id. Override with OPENROUTER_MODEL.
	defaultModel = "meta-llama/llama-3.3-70b-instruct"

	// Attribution headers (optional, for OpenRouter's app leaderboard).
	httpReferer = "https://github.com/futurebuildai/ai-lm"
	appTitle    = "AI_LM"
)

// ErrNotConfigured is returned by chat methods when no API key is set.
var ErrNotConfigured = errors.New("ai: OpenRouter API key not configured")

// Client is an OpenRouter chat client.
type Client struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client
}

// NewClient builds an OpenRouter client. An empty apiKey is valid: the client
// is then "not configured" and Generate returns ErrNotConfigured. baseURL and
// model fall back to OpenRouter defaults / an open-weight model when empty.
func NewClient(apiKey, baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if model == "" {
		model = defaultModel
	}
	return &Client{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// Configured reports whether an API key is present. Callers gate on this and
// choose their own degradation (the transport guards too as a backstop).
func (c *Client) Configured() bool { return c != nil && c.apiKey != "" }

// Model returns the configured model id (for surfacing in responses/logs).
func (c *Client) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}

// --- wire types (OpenAI-compatible) -----------------------------------------

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Model string `json:"model"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error,omitempty"`
}

// Generate runs a single system+user chat completion and returns the assistant
// text. maxTokens caps the completion (0 ⇒ provider default). Returns
// ErrNotConfigured when no key is set.
func (c *Client) Generate(ctx context.Context, systemPrompt, userPrompt string, maxTokens int) (string, error) {
	if !c.Configured() {
		return "", ErrNotConfigured
	}

	reqBody := chatRequest{
		Model:       c.model,
		MaxTokens:   maxTokens,
		Temperature: 0.3,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("ai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("HTTP-Referer", httpReferer)
	httpReq.Header.Set("X-Title", appTitle)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("ai: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("ai: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var er chatResponse
		if json.Unmarshal(respBody, &er) == nil && er.Error != nil {
			return "", fmt.Errorf("ai: OpenRouter error (%d): %s", resp.StatusCode, er.Error.Message)
		}
		return "", fmt.Errorf("ai: OpenRouter error (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var cr chatResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return "", fmt.Errorf("ai: parse response: %w", err)
	}
	if cr.Error != nil {
		return "", fmt.Errorf("ai: OpenRouter error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("ai: empty response (no choices)")
	}
	text := strings.TrimSpace(cr.Choices[0].Message.Content)
	if text == "" {
		slog.Warn("ai: model returned empty content", "model", cr.Model)
	}
	return text, nil
}
