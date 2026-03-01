package core

import (
	"context"
	"encoding/json"
)

type ResponseToolSpec struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type PolicyRequest[S any] struct {
	State     S
	Iteration int
	Prompt    string
}

type ToolPolicy struct {
	AllowedToolNames []string
	Mode             string
	PolicyVersion    string
}

type PolicyResolver[S any] interface {
	Resolve(ctx context.Context, req PolicyRequest[S]) (ToolPolicy, error)
}

type Tool[S any] interface {
	Name() string
	Spec() ResponseToolSpec
	Execute(ctx context.Context, state S, input json.RawMessage, callID string) (string, *ToolError)
}
