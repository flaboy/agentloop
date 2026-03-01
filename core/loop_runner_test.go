package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type loopTestState struct {
	Session string
}

type staticPolicyResolver struct {
	calls int
	list  []string
}

func (r *staticPolicyResolver) Resolve(_ context.Context, req PolicyRequest[loopTestState]) (ToolPolicy, error) {
	r.calls++
	return ToolPolicy{
		AllowedToolNames: append([]string{}, r.list...),
		Mode:             "test",
		PolicyVersion:    "v1",
	}, nil
}

type fakeResponsesClient struct {
	calls    int
	requests []CreateResponseRequest
}

func (c *fakeResponsesClient) CreateResponse(_ context.Context, req CreateResponseRequest) (*CreateResponseResult, error) {
	c.calls++
	c.requests = append(c.requests, req)
	if c.calls == 1 {
		return &CreateResponseResult{
			ID: "resp_1",
			ToolCalls: []ToolCall{{
				CallID:    "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hi"}`),
			}},
		}, nil
	}
	return &CreateResponseResult{ID: "resp_2", FinalText: "done"}, nil
}

type echoTool struct{}

func (echoTool) Name() string { return "echo" }

func (echoTool) Spec() ResponseToolSpec {
	return ResponseToolSpec{Type: "function", Name: "echo"}
}

func (echoTool) Execute(_ context.Context, _ loopTestState, _ json.RawMessage, _ string) (string, *ToolError) {
	return `{"ok":true}`, nil
}

func TestLoopRunner_ReResolvePolicyEveryIteration(t *testing.T) {
	resolver := &staticPolicyResolver{list: []string{"echo"}}
	registry := NewToolRegistry[loopTestState]()
	if err := registry.Register(echoTool{}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	client := &fakeResponsesClient{}
	runner := NewLoopRunner(client, registry, resolver, LoopRunnerOptions{MaxIterations: 4})

	out, err := runner.Run(context.Background(), loopTestState{Session: "s1"}, "hello")
	if err != nil {
		t.Fatalf("runner failed: %v", err)
	}
	if strings.TrimSpace(out) != "done" {
		t.Fatalf("unexpected output: %q", out)
	}
	if resolver.calls != 2 {
		t.Fatalf("resolver should be called each iteration, got %d", resolver.calls)
	}
	if len(client.requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(client.requests))
	}
}

func TestLoopRunner_RejectToolNotInAllowedList(t *testing.T) {
	resolver := &staticPolicyResolver{list: []string{}}
	registry := NewToolRegistry[loopTestState]()
	if err := registry.Register(echoTool{}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	client := &fakeResponsesClient{}
	runner := NewLoopRunner(client, registry, resolver, LoopRunnerOptions{MaxIterations: 4})

	_, err := runner.Run(context.Background(), loopTestState{Session: "s1"}, "hello")
	if err != nil {
		t.Fatalf("runner failed: %v", err)
	}
	if len(client.requests) < 2 {
		t.Fatalf("expected roundtrip request")
	}
	if !strings.Contains(summarizeResponseInput(client.requests[1].Input), "TOOL_NOT_ENABLED_IN_MODE") {
		t.Fatalf("expected tool reject output in second request summary")
	}
}
