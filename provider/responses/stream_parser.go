package responses

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flaboy/agentloop/core"
)

type StreamAccumulator struct {
	result core.CreateResponseResult
}

func NewStreamAccumulator() *StreamAccumulator {
	return &StreamAccumulator{}
}

func (a *StreamAccumulator) ApplyEvent(data []byte) error {
	if a == nil {
		return fmt.Errorf("stream accumulator is nil")
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	var event struct {
		Type       string `json:"type"`
		Delta      string `json:"delta"`
		ResponseID string `json:"response_id"`
		Response   struct {
			ID string `json:"id"`
		} `json:"response"`
		Item struct {
			Type      string          `json:"type"`
			ID        string          `json:"id"`
			CallID    string          `json:"call_id"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(trimmed), &event); err != nil {
		return fmt.Errorf("invalid stream event: %w", err)
	}
	if strings.TrimSpace(a.result.ID) == "" {
		a.result.ID = strings.TrimSpace(event.Response.ID)
		if a.result.ID == "" {
			a.result.ID = strings.TrimSpace(event.ResponseID)
		}
	}
	switch strings.TrimSpace(event.Type) {
	case "response.output_text.delta":
		a.result.FinalText += event.Delta
	case "response.output_item.added", "response.output_item.done":
		if strings.TrimSpace(event.Item.Type) != "function_call" {
			return nil
		}
		call := core.ToolCall{
			ID:        strings.TrimSpace(event.Item.ID),
			CallID:    strings.TrimSpace(event.Item.CallID),
			Name:      strings.TrimSpace(event.Item.Name),
			Arguments: event.Item.Arguments,
		}
		if len(call.Arguments) == 0 {
			call.Arguments = json.RawMessage("{}")
		}
		a.result.ToolCalls = append(a.result.ToolCalls, call)
	}
	return nil
}

func (a *StreamAccumulator) Result() core.CreateResponseResult {
	if a == nil {
		return core.CreateResponseResult{}
	}
	out := a.result
	out.ToolCalls = append([]core.ToolCall{}, a.result.ToolCalls...)
	return out
}
