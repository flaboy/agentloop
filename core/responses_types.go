package core

import (
	"context"
	"strings"
)

type ResponseInput struct {
	Text  string
	Items []ResponseInputItem
}

func NewResponseInputText(text string) ResponseInput {
	return ResponseInput{Text: strings.TrimSpace(text)}
}

func NewResponseInputItems(items []ResponseInputItem) ResponseInput {
	if len(items) == 0 {
		return ResponseInput{Items: []ResponseInputItem{}}
	}
	out := make([]ResponseInputItem, len(items))
	copy(out, items)
	return ResponseInput{Items: out}
}

type ResponseInputContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type ResponseInputItem struct {
	Type      string                     `json:"type"`
	Role      string                     `json:"role,omitempty"`
	Content   []ResponseInputContentPart `json:"content,omitempty"`
	ID        string                     `json:"id,omitempty"`
	CallID    string                     `json:"call_id,omitempty"`
	Name      string                     `json:"name,omitempty"`
	Arguments string                     `json:"arguments,omitempty"`
	Output    string                     `json:"output,omitempty"`
}

type CreateResponseRequest struct {
	Model              string             `json:"model"`
	Input              ResponseInput      `json:"input"`
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
	Arguments  string
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
