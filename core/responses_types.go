package core

import (
	"context"
	"encoding/json"
	"strings"
)

type CreateResponseRequest struct {
	Model              string             `json:"model"`
	Input              any                `json:"input"`
	Tools              []ResponseToolSpec `json:"tools,omitempty"`
	PreviousResponseID string             `json:"previous_response_id,omitempty"`
	Store              *bool              `json:"store,omitempty"`
	Stream             bool               `json:"stream,omitempty"`
}

type ToolCall struct {
	ID         string
	CallID     string
	ResponseID string
	Name       string
	Arguments  json.RawMessage
}

type CreateResponseResult struct {
	ID         string
	FinalText  string
	ToolCalls  []ToolCall
	EventTrace []string
}

func (r CreateResponseResult) HasFinalText() bool {
	return strings.TrimSpace(r.FinalText) != ""
}

type ResponsesAPI interface {
	CreateResponse(ctx context.Context, req CreateResponseRequest) (*CreateResponseResult, error)
}
