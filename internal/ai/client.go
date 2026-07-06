package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	anthropicAPIURL  = "https://api.anthropic.com/v1/messages"
	anthropicModel   = "claude-sonnet-4-6"
	anthropicVersion = "2023-06-01"
)

// Client wraps calls to the Claude API. Construct one per service
// instance (it's safe for concurrent use — http.Client is).
type Client struct {
	apiKey     string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second, // AI generations run ~15-25s
		},
	}
}

type messageRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messageResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// Generate sends a system prompt + user input to Claude and returns the
// raw text response. Callers (e.g. internal/http/ai.go, not yet
// scaffolded) are responsible for parsing that text as JSON against the
// schema documented in the AI Prompt Library, and for retrying on a
// schema mismatch — this function does not validate output shape.
func (c *Client) Generate(ctx context.Context, systemPrompt, userInput string) (string, error) {
	reqBody := messageRequest{
		Model:     anthropicModel,
		MaxTokens: 2048,
		System:    systemPrompt,
		Messages:  []message{{Role: "user", Content: userInput}},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling Claude API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Claude API returned status %d", resp.StatusCode)
	}

	var parsed messageResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}
	if len(parsed.Content) == 0 {
		return "", fmt.Errorf("empty response content")
	}

	// Defensive: strip markdown fences if the model adds them despite
	// being told not to — see AI Prompt Library's guardrails section.
	text := strings.TrimSpace(parsed.Content[0].Text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text), nil
}
