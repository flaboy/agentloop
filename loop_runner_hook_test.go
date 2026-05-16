package agentloop

import (
	"context"
	"fmt"
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
type hookTestLargeOutputTool struct{}

func (hookTestTool) Name() string { return "echo" }

func (hookTestTool) Spec() core.ResponseToolSpec {
	return core.ResponseToolSpec{Type: "function", Name: "echo"}
}

func (hookTestTool) Execute(_ context.Context, _ struct{}, _ string, _ string) (string, *core.ToolError) {
	return "tool-output", nil
}

func (hookTestTool) Cancel(_ context.Context, _ struct{}, _ string, _ string) *core.ToolError {
	return nil
}

func (hookTestOtherTool) Name() string { return "other" }

func (hookTestOtherTool) Spec() core.ResponseToolSpec {
	return core.ResponseToolSpec{Type: "function", Name: "other"}
}

func (hookTestOtherTool) Execute(_ context.Context, _ struct{}, _ string, _ string) (string, *core.ToolError) {
	return "other-output", nil
}

func (hookTestOtherTool) Cancel(_ context.Context, _ struct{}, _ string, _ string) *core.ToolError {
	return nil
}

func (hookTestLargeOutputTool) Name() string { return "large" }

func (hookTestLargeOutputTool) Spec() core.ResponseToolSpec {
	return core.ResponseToolSpec{Type: "function", Name: "large"}
}

func (hookTestLargeOutputTool) Execute(_ context.Context, _ struct{}, _ string, _ string) (string, *core.ToolError) {
	return strings.Repeat("x", 400), nil
}

func (hookTestLargeOutputTool) Cancel(_ context.Context, _ struct{}, _ string, _ string) *core.ToolError {
	return nil
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

func TestRoundtripHookCanRequestCompactionRewrite(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{
		{ID: "resp-1", ToolCalls: []core.ToolCall{{CallID: "call-1", Name: "echo", Arguments: "{}"}}},
		{ID: "resp-2", FinalText: "done"},
	}}
	registry := core.NewToolRegistry[struct{}]()
	if err := registry.Register(runnerStateTool{}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	runner := NewLoopRunner(client, registry, LoopRunnerOptions{MaxIterations: 3})
	runner.RegisterCompactionDelegate(func(input CompactionDelegateInput) (CompactionDelegateOutput, error) {
		return CompactionDelegateOutput{
			NeedCompaction:        true,
			ForceHistoryMode:      HistoryModeLocalReplay,
			ResetPreviousResponse: true,
			RewriteRequest: &ContextBuildRequest{
				Inbound: input.OriginalContextRequest.Inbound,
				PrebuiltRequest: &core.CreateResponseRequest{
					Model: "gpt-test",
					Input: core.NewResponseInputItems([]core.ResponseInputItem{
						buildSystemMessageInputItem("summary"),
						buildUserMessageInputItem("hello"),
					}),
				},
				PrebuiltHistoryInputItems: []core.ResponseInputItem{
					buildSystemMessageInputItem("summary"),
					buildUserMessageInputItem("hello"),
				},
				PrebuiltAppliedHistoryMode: HistoryModeLocalReplay,
			},
		}, nil
	})

	out, err := runner.RunWithContextResult(context.Background(), ContextBuildRequest{
		Inbound:            InboundMessage{Role: "user", Content: "hello"},
		HistoryMode:        HistoryModeProviderState,
		PreviousResponseID: "resp-0",
		Store:              boolPtr(true),
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.AppliedHistoryMode != HistoryModeLocalReplay {
		t.Fatalf("expected local replay after compaction, got %q", out.AppliedHistoryMode)
	}
	if len(client.requests) < 2 {
		t.Fatalf("expected second request after tool roundtrip, got %d requests", len(client.requests))
	}
	second := client.requests[1]
	if second.PreviousResponseID != "" {
		t.Fatalf("expected previous_response_id reset, got %#v", second)
	}
	if len(second.Input.Items) < 1 || len(second.Input.Items[0].Content) < 1 || second.Input.Items[0].Content[0].Text != "summary" {
		t.Fatalf("expected compacted summary in second request, got %#v", second.Input.Items)
	}
}

func TestCompactionDelegateReceivesPostToolOutputTokenLength(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{
		{ID: "resp-1", ToolCalls: []core.ToolCall{{CallID: "call-1", Name: "large", Arguments: "{}"}}},
		{ID: "resp-2", FinalText: "done"},
	}}
	registry := core.NewToolRegistry[struct{}]()
	if err := registry.Register(hookTestLargeOutputTool{}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	runner := NewLoopRunner(client, registry, LoopRunnerOptions{MaxIterations: 3})
	var gotTokens int64
	var gotTrigger CompactionTrigger
	runner.RegisterCompactionDelegate(func(input CompactionDelegateInput) (CompactionDelegateOutput, error) {
		gotTokens = input.ContextTokens
		gotTrigger = input.Trigger
		return CompactionDelegateOutput{}, nil
	})

	out, err := runner.RunWithContextResult(context.Background(), ContextBuildRequest{
		Inbound: InboundMessage{Role: "user", Content: "run tool"},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected output: %q", out.FinalText)
	}
	if gotTrigger != CompactionTriggerThreshold {
		t.Fatalf("expected threshold trigger, got %q", gotTrigger)
	}
	if gotTokens < 100 {
		t.Fatalf("expected tool output to be counted, got %d", gotTokens)
	}
}

func TestLoopRunner_RewritesContextBetweenRoundtripsWhenDelegateRequestsCompaction(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{
		{ID: "resp-1", ToolCalls: []core.ToolCall{{CallID: "call-1", Name: "echo", Arguments: "{}"}}},
		{ID: "resp-2", FinalText: "done"},
	}}
	registry := core.NewToolRegistry[struct{}]()
	if err := registry.Register(runnerStateTool{}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	runner := NewLoopRunner(client, registry, LoopRunnerOptions{MaxIterations: 3})
	runner.RegisterCompactionDelegate(func(input CompactionDelegateInput) (CompactionDelegateOutput, error) {
		if input.Iteration != 1 {
			return CompactionDelegateOutput{}, nil
		}
		return CompactionDelegateOutput{
			NeedCompaction:        true,
			ForceHistoryMode:      HistoryModeLocalReplay,
			ResetPreviousResponse: true,
			RewriteRequest: &ContextBuildRequest{
				Inbound: input.OriginalContextRequest.Inbound,
				PrebuiltRequest: &core.CreateResponseRequest{
					Model: "gpt-test",
					Input: core.NewResponseInputItems([]core.ResponseInputItem{
						buildSystemMessageInputItem("compacted summary"),
						buildUserMessageInputItem("hello"),
					}),
				},
				PrebuiltHistoryInputItems: []core.ResponseInputItem{
					buildSystemMessageInputItem("compacted summary"),
					buildUserMessageInputItem("hello"),
				},
				PrebuiltAppliedHistoryMode: HistoryModeLocalReplay,
			},
		}, nil
	})

	_, err := runner.RunWithContextResult(context.Background(), ContextBuildRequest{
		Inbound:            InboundMessage{Role: "user", Content: "hello"},
		HistoryMode:        HistoryModeProviderState,
		PreviousResponseID: "resp-0",
		Store:              boolPtr(true),
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(client.requests) < 2 {
		t.Fatalf("expected second request after tool roundtrip, got %d requests", len(client.requests))
	}
	second := client.requests[1]
	if second.PreviousResponseID != "" {
		t.Fatalf("expected compacted second request to clear previous response id, got %#v", second)
	}
	if len(second.Input.Items) < 1 || len(second.Input.Items[0].Content) < 1 || second.Input.Items[0].Content[0].Text != "compacted summary" {
		t.Fatalf("expected compacted second request, got %#v", second.Input.Items)
	}
}

func TestCompactionDelegate_FailsFastWhenRewriteRequestMissing(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{
		{ID: "resp-1", ToolCalls: []core.ToolCall{{CallID: "call-1", Name: "echo", Arguments: "{}"}}},
	}}
	registry := core.NewToolRegistry[struct{}]()
	if err := registry.Register(runnerStateTool{}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	runner := NewLoopRunner(client, registry, LoopRunnerOptions{MaxIterations: 2})
	runner.RegisterCompactionDelegate(func(input CompactionDelegateInput) (CompactionDelegateOutput, error) {
		return CompactionDelegateOutput{NeedCompaction: true, ForceHistoryMode: HistoryModeLocalReplay}, nil
	})

	_, err := runner.RunWithContextResult(context.Background(), ContextBuildRequest{
		Inbound: InboundMessage{Role: "user", Content: "hello"},
	})
	if err == nil || !strings.Contains(err.Error(), "rewrite request") {
		t.Fatalf("expected missing rewrite request error, got %v", err)
	}
}

func TestCompactionDelegate_ForceHistoryModeOverridesOriginalRequest(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{
		{ID: "resp-1", ToolCalls: []core.ToolCall{{CallID: "call-1", Name: "echo", Arguments: "{}"}}},
		{ID: "resp-2", FinalText: "done"},
	}}
	registry := core.NewToolRegistry[struct{}]()
	if err := registry.Register(runnerStateTool{}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	runner := NewLoopRunner(client, registry, LoopRunnerOptions{MaxIterations: 3})
	runner.RegisterCompactionDelegate(func(input CompactionDelegateInput) (CompactionDelegateOutput, error) {
		req := ContextBuildRequest{
			Inbound:                    input.OriginalContextRequest.Inbound,
			PrebuiltRequest:            &core.CreateResponseRequest{Input: core.NewResponseInputItems([]core.ResponseInputItem{buildUserMessageInputItem("hello")})},
			PrebuiltHistoryInputItems:  []core.ResponseInputItem{buildUserMessageInputItem("hello")},
			PrebuiltAppliedHistoryMode: HistoryModeProviderState,
		}
		return CompactionDelegateOutput{
			NeedCompaction:   true,
			RewriteRequest:   &req,
			ForceHistoryMode: HistoryModeLocalReplay,
		}, nil
	})

	out, err := runner.RunWithContextResult(context.Background(), ContextBuildRequest{
		Inbound:            InboundMessage{Role: "user", Content: "hello"},
		HistoryMode:        HistoryModeProviderState,
		PreviousResponseID: "resp-0",
		Store:              boolPtr(true),
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.AppliedHistoryMode != HistoryModeLocalReplay {
		t.Fatalf("expected forced local replay, got %q", out.AppliedHistoryMode)
	}
}

func TestCompactionDelegate_DoesNotRunOnTerminalResponse(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{{ID: "resp-1", FinalText: "done"}}}
	runner := NewLoopRunner(client, nil, LoopRunnerOptions{MaxIterations: 2})
	calls := 0
	runner.RegisterCompactionDelegate(func(input CompactionDelegateInput) (CompactionDelegateOutput, error) {
		calls++
		return CompactionDelegateOutput{}, fmt.Errorf("should not be called")
	})

	out, err := runner.RunWithContextResult(context.Background(), ContextBuildRequest{
		Inbound: InboundMessage{Role: "user", Content: "hello"},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected output: %q", out.FinalText)
	}
	if calls != 0 {
		t.Fatalf("expected no compaction delegate calls, got %d", calls)
	}
}

func boolPtr(v bool) *bool { return &v }
