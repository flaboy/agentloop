package core

import (
	"context"
	"encoding/json"
	"strings"
)

type ResponseToolSpec struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  ResponseToolParameters `json:"parameters,omitempty"`
}

type ResponseToolParameters struct {
	Type       string                 `json:"type"`
	Properties []ResponseToolProperty `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
}

type ResponseToolProperty struct {
	Name   string
	Schema ResponseToolSchema
}

type ResponseToolSchema struct {
	Type    string   `json:"type,omitempty"`
	Enum    []string `json:"enum,omitempty"`
	Minimum *int     `json:"minimum,omitempty"`
	Maximum *int     `json:"maximum,omitempty"`
}

func (p ResponseToolParameters) MarshalJSON() ([]byte, error) {
	var out strings.Builder
	out.WriteByte('{')
	written := false
	writeField := func(name string, value []byte) {
		if written {
			out.WriteByte(',')
		}
		written = true
		out.WriteString(`"`)
		out.WriteString(name)
		out.WriteString(`":`)
		out.Write(value)
	}

	if value := strings.TrimSpace(p.Type); value != "" {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		writeField("type", raw)
	}
	if len(p.Properties) > 0 {
		raw, err := marshalToolProperties(p.Properties)
		if err != nil {
			return nil, err
		}
		writeField("properties", raw)
	}
	if len(p.Required) > 0 {
		raw, err := json.Marshal(p.Required)
		if err != nil {
			return nil, err
		}
		writeField("required", raw)
	}
	out.WriteByte('}')
	return []byte(out.String()), nil
}

func marshalToolProperties(properties []ResponseToolProperty) ([]byte, error) {
	var out strings.Builder
	out.WriteByte('{')
	written := false
	for _, property := range properties {
		name := strings.TrimSpace(property.Name)
		if name == "" {
			continue
		}
		key, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(property.Schema)
		if err != nil {
			return nil, err
		}
		if written {
			out.WriteByte(',')
		}
		written = true
		out.Write(key)
		out.WriteByte(':')
		out.Write(value)
	}
	out.WriteByte('}')
	return []byte(out.String()), nil
}

func IntPtr(v int) *int {
	return &v
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
	Execute(ctx context.Context, state S, input string, callID string) (string, *ToolError)
	Cancel(ctx context.Context, state S, input string, callID string) *ToolError
}
