package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/flaboy/agentloop/core"
)

type OpenAIConfig struct {
	BaseURL string
	Model   string
	APIKey  string
}

type Client struct {
	cfg        OpenAIConfig
	httpClient *http.Client
}

func NewClient(cfg OpenAIConfig, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{cfg: cfg, httpClient: httpClient}
}

func (c *Client) CreateResponse(ctx context.Context, req core.CreateResponseRequest) (*core.CreateResponseResult, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("responses client is not initialized")
	}
	if strings.TrimSpace(req.Model) == "" {
		req.Model = strings.TrimSpace(c.cfg.Model)
	}
	req.Stream = false

	rawReq, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := strings.TrimRight(strings.TrimSpace(c.cfg.BaseURL), "/") + "/responses"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawReq))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(c.cfg.APIKey); key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}

	httpRes, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http do: %w", err)
	}
	defer httpRes.Body.Close()

	body, err := io.ReadAll(httpRes.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if httpRes.StatusCode >= 300 {
		return nil, fmt.Errorf("responses api failed status=%d body=%s", httpRes.StatusCode, strings.TrimSpace(string(body)))
	}
	return parseResponseResult(body)
}

func parseResponseResult(body []byte) (*core.CreateResponseResult, error) {
	var raw struct {
		ID     string `json:"id"`
		Output []struct {
			Type      string          `json:"type"`
			ID        string          `json:"id"`
			CallID    string          `json:"call_id"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid responses payload: %w", err)
	}
	out := &core.CreateResponseResult{ID: strings.TrimSpace(raw.ID)}
	for _, item := range raw.Output {
		switch strings.TrimSpace(item.Type) {
		case "message":
			for _, part := range item.Content {
				if strings.TrimSpace(part.Type) == "output_text" {
					out.FinalText += part.Text
				}
			}
		case "function_call":
			call := core.ToolCall{
				ID:        strings.TrimSpace(item.ID),
				CallID:    strings.TrimSpace(item.CallID),
				Name:      strings.TrimSpace(item.Name),
				Arguments: item.Arguments,
			}
			if len(call.Arguments) == 0 {
				call.Arguments = json.RawMessage("{}")
			}
			out.ToolCalls = append(out.ToolCalls, call)
		}
	}
	return out, nil
}
