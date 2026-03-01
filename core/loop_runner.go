package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	ID        string
	FinalText string
	ToolCalls []ToolCall
}

func (r CreateResponseResult) HasFinalText() bool {
	return strings.TrimSpace(r.FinalText) != ""
}

type ResponsesAPI interface {
	CreateResponse(ctx context.Context, req CreateResponseRequest) (*CreateResponseResult, error)
}

type LoopRunnerOptions struct {
	MaxIterations int
}

type LoopRunner[S any] struct {
	client   ResponsesAPI
	tools    *ToolRegistry[S]
	resolver PolicyResolver[S]
	options  LoopRunnerOptions
}

func NewLoopRunner[S any](client ResponsesAPI, tools *ToolRegistry[S], resolver PolicyResolver[S], options LoopRunnerOptions) *LoopRunner[S] {
	if options.MaxIterations <= 0 {
		options.MaxIterations = 8
	}
	return &LoopRunner[S]{client: client, tools: tools, resolver: resolver, options: options}
}

func (r *LoopRunner[S]) Run(ctx context.Context, state S, prompt string) (string, error) {
	if r == nil || r.client == nil {
		return "", errors.New("loop runner client is required")
	}
	if r.resolver == nil {
		return "", errors.New("policy resolver is required")
	}
	if r.tools == nil {
		return "", errors.New("tool registry is required")
	}

	prompt = strings.TrimSpace(prompt)
	history := []map[string]any{buildUserMessageInputItem(prompt)}
	req := CreateResponseRequest{Input: cloneResponseInputItems(history)}

	for i := 0; i < r.options.MaxIterations; i++ {
		policy, err := r.resolver.Resolve(ctx, PolicyRequest[S]{
			State:     state,
			Iteration: i + 1,
			Prompt:    prompt,
		})
		if err != nil {
			return "", fmt.Errorf("resolve policy failed iteration=%d: %w", i+1, err)
		}
		allowed := policyAllowedSet(policy)
		req.Tools = r.tools.SpecsByNames(policy.AllowedToolNames)

		if err := ValidateResponseInputInvariants(req.Input); err != nil {
			return "", fmt.Errorf("input invariant failed iteration=%d: %w", i+1, err)
		}
		res, err := r.client.CreateResponse(ctx, req)
		if err != nil {
			return "", fmt.Errorf("responses request failed iteration=%d: %w", i+1, err)
		}
		if res == nil {
			return "", fmt.Errorf("responses request failed iteration=%d: nil response", i+1)
		}
		if res.HasFinalText() {
			return res.FinalText, nil
		}
		if len(res.ToolCalls) == 0 {
			return "", fmt.Errorf("responses api returned no output_text and no tool_calls iteration=%d", i+1)
		}

		for _, call := range res.ToolCalls {
			callID := strings.TrimSpace(call.CallID)
			if callID == "" {
				return "", fmt.Errorf("responses tool call missing call_id iteration=%d", i+1)
			}

			out := ""
			if _, ok := allowed[strings.TrimSpace(call.Name)]; !ok {
				out = mustMarshalToolError(NewToolError(
					"TOOL_NOT_ENABLED_IN_MODE",
					fmt.Sprintf("tool %q is not in allowed_tools", strings.TrimSpace(call.Name)),
				))
			} else {
				toolOut, toolErr := r.tools.Execute(ctx, state, call.Name, call.Arguments, callID)
				if toolErr != nil {
					out = mustMarshalToolError(toolErr)
				} else {
					out = toolOut
				}
			}

			history = append(history,
				map[string]any{
					"type":      "function_call",
					"call_id":   callID,
					"name":      strings.TrimSpace(call.Name),
					"arguments": strings.TrimSpace(string(call.Arguments)),
				},
				map[string]any{
					"type":    "function_call_output",
					"call_id": callID,
					"output":  out,
				},
			)
		}
		req.Input = cloneResponseInputItems(history)
	}

	return "", fmt.Errorf("responses loop exceeded max iterations: %d", r.options.MaxIterations)
}

func policyAllowedSet(policy ToolPolicy) map[string]struct{} {
	set := map[string]struct{}{}
	for _, item := range policy.AllowedToolNames {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}
	return set
}

func mustMarshalToolError(err *ToolError) string {
	if err == nil {
		return `{"ok":false,"error":{"code":"UNKNOWN","message":"unknown tool error"}}`
	}
	raw, marshalErr := json.Marshal(map[string]any{
		"ok": false,
		"error": map[string]any{
			"code":    strings.TrimSpace(err.Code),
			"message": strings.TrimSpace(err.Message),
		},
	})
	if marshalErr != nil {
		return `{"ok":false,"error":{"code":"MARSHAL_FAILED","message":"failed to marshal tool error"}}`
	}
	return string(raw)
}

func summarizeResponseInput(input any) string {
	raw, err := json.Marshal(input)
	if err != nil {
		return fmt.Sprintf("%T", input)
	}
	text := strings.TrimSpace(string(raw))
	if len(text) > 1200 {
		return text[:1200] + "...(truncated)"
	}
	return text
}

func cloneResponseInputItems(in []map[string]any) []map[string]any {
	if len(in) == 0 {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(in))
	for _, item := range in {
		out = append(out, item)
	}
	return out
}

func buildUserMessageInputItem(text string) map[string]any {
	return map[string]any{
		"type": "message",
		"role": "user",
		"content": []map[string]any{
			{
				"type": "input_text",
				"text": strings.TrimSpace(text),
			},
		},
	}
}
