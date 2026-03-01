package agentloop

import (
	"context"
	"reflect"
	"strings"
	"testing"

	core "github.com/flaboy/agentloop/core"
)

type hookTestClient struct {
	responses []core.CreateResponseResult
	requests  []core.CreateResponseRequest
}

func (c *hookTestClient) CreateResponse(_ context.Context, req core.CreateResponseRequest) (*core.CreateResponseResult, error) {
	c.requests = append(c.requests, req)
	if len(c.responses) == 0 {
		return &core.CreateResponseResult{FinalText: "done"}, nil
	}
	res := c.responses[0]
	c.responses = c.responses[1:]
	return &res, nil
}

type hookTestTool struct{}
type hookTestOtherTool struct{}

func (hookTestTool) Name() string { return "echo" }

func (hookTestTool) Spec() core.ResponseToolSpec {
	return core.ResponseToolSpec{Type: "function", Name: "echo"}
}

func (hookTestTool) Execute(_ context.Context, _ struct{}, _ string, _ string) (string, *core.ToolError) {
	return "tool-output", nil
}

func (hookTestOtherTool) Name() string { return "other" }

func (hookTestOtherTool) Spec() core.ResponseToolSpec {
	return core.ResponseToolSpec{Type: "function", Name: "other"}
}

func (hookTestOtherTool) Execute(_ context.Context, _ struct{}, _ string, _ string) (string, *core.ToolError) {
	return "other-output", nil
}

func TestLoopRunner_ModelHookWrapsModelCall(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{{FinalText: "hello"}}}
	runner := NewLoopRunner(client, nil, LoopRunnerOptions{MaxIterations: 2})

	order := []string{}
	runner.RegisterHook(HookPointModelCall, func(_ *HookContext, next NextFunc) error {
		order = append(order, "pre")
		err := next()
		order = append(order, "post")
		return err
	})

	out, err := runner.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out != "hello" {
		t.Fatalf("unexpected output: %q", out)
	}
	if !reflect.DeepEqual(order, []string{"pre", "post"}) {
		t.Fatalf("unexpected hook order: %#v", order)
	}
}

func TestLoopRunner_ToolHookCanRewriteToolOutput(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{
		{ID: "resp-1", ToolCalls: []core.ToolCall{{CallID: "call-1", Name: "echo", Arguments: "{}"}}},
		{ID: "resp-2", FinalText: "done"},
	}}
	registry := core.NewToolRegistry[struct{}]()
	if err := registry.Register(hookTestTool{}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	runner := NewLoopRunner(client, registry, LoopRunnerOptions{MaxIterations: 3})

	runner.RegisterHook(HookPointToolCall, func(ctx *HookContext, next NextFunc) error {
		if ctx.ToolCall == nil {
			t.Fatalf("tool call is nil")
		}
		err := next()
		if err != nil {
			return err
		}
		if ctx.ToolOutput == nil {
			t.Fatalf("tool output is nil")
		}
		rewritten := "rewritten-output"
		ctx.ToolOutput = &rewritten
		return nil
	})

	out, err := runner.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out != "done" {
		t.Fatalf("unexpected output: %q", out)
	}
	if len(client.requests) < 2 {
		t.Fatalf("expected at least 2 model requests, got %d", len(client.requests))
	}
	items := client.requests[1].Input.Items
	if len(items) == 0 {
		t.Fatalf("second request has no items")
	}
	last := items[len(items)-1]
	if last.Type != "function_call_output" {
		t.Fatalf("expected last item function_call_output, got %q", last.Type)
	}
	if last.Output != "rewritten-output" {
		t.Fatalf("unexpected rewritten output: %q", last.Output)
	}
}

func TestLoopRunner_HookMustCallNext(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{{FinalText: "hello"}}}
	runner := NewLoopRunner(client, nil, LoopRunnerOptions{MaxIterations: 2})

	runner.RegisterHook(HookPointModelCall, func(_ *HookContext, _ NextFunc) error {
		return nil
	})

	_, err := runner.Run(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error when hook does not call next")
	}
}

func TestLoopRunner_ModelHookAllowedToolNamesRestrictsExecution(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{
		{ID: "resp-1", ToolCalls: []core.ToolCall{{CallID: "call-1", Name: "other", Arguments: "{}"}}},
		{ID: "resp-2", FinalText: "done"},
	}}
	registry := core.NewToolRegistry[struct{}]()
	if err := registry.Register(hookTestTool{}); err != nil {
		t.Fatalf("register echo tool failed: %v", err)
	}
	if err := registry.Register(hookTestOtherTool{}); err != nil {
		t.Fatalf("register other tool failed: %v", err)
	}
	runner := NewLoopRunner(client, registry, LoopRunnerOptions{MaxIterations: 2})
	runner.RegisterHook(HookPointModelCall, func(ctx *HookContext, next NextFunc) error {
		ctx.SetAllowedToolNames([]string{"echo"})
		return next()
	})
	out, err := runner.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out != "done" {
		t.Fatalf("unexpected output: %q", out)
	}
	if len(client.requests) < 2 {
		t.Fatalf("expected at least 2 requests, got %d", len(client.requests))
	}
	items := client.requests[1].Input.Items
	if len(items) == 0 {
		t.Fatal("second request has no input items")
	}
	last := items[len(items)-1]
	if !strings.Contains(last.Output, "TOOL_NOT_ENABLED_IN_MODE") {
		t.Fatalf("expected tool restriction marker in output, got: %q", last.Output)
	}
}
