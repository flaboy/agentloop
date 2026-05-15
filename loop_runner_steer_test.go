package agentloop

import (
	"context"
	"strings"
	"testing"

	core "github.com/flaboy/agentloop/core"
)

func TestSteerDelegate_RewritesBeforeTerminalFinal(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{
		{ID: "resp-1", FinalText: "old final"},
		{ID: "resp-2", FinalText: "new final"},
	}}
	runner := NewLoopRunner(client, nil, LoopRunnerOptions{MaxIterations: 3})
	calls := 0
	applied := false
	runner.RegisterSteerDelegate(func(ctx context.Context, input SteerDelegateInput) (SteerDelegateOutput, error) {
		calls++
		if applied || input.Boundary != SteerBoundaryAfterModelResponseBeforeFinal {
			return SteerDelegateOutput{}, nil
		}
		applied = true
		return SteerDelegateOutput{
			Events: []SteerEvent{{
				ID:         "steer_1",
				SourceType: "user",
				IntentType: "correction",
				Content:    "change direction",
			}},
			RewriteRequest: &ContextBuildRequest{
				Inbound: InboundMessage{Role: "user", Content: "change direction"},
			},
			ResetPreviousResponse: true,
			ForceHistoryMode:      HistoryModeLocalReplay,
		}, nil
	})

	out, err := runner.RunWithContextResult(context.Background(), ContextBuildRequest{
		Inbound: InboundMessage{Role: "user", Content: "start"},
	})

	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "new final" {
		t.Fatalf("expected rewritten final text, got %q", out.FinalText)
	}
	if calls < 1 {
		t.Fatalf("expected steer delegate to be called")
	}
	if len(client.requests) != 2 {
		t.Fatalf("expected 2 model requests, got %d", len(client.requests))
	}
}

func TestSteerDelegate_AppendsAfterToolRoundtrip(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{
		{ID: "resp-1", ToolCalls: []core.ToolCall{{CallID: "call-1", Name: "echo", Arguments: "{}"}}},
		{ID: "resp-2", FinalText: "done with steer"},
	}}
	registry := core.NewToolRegistry[struct{}]()
	if err := registry.Register(runnerStateTool{}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	runner := NewLoopRunner(client, registry, LoopRunnerOptions{MaxIterations: 3})
	runner.RegisterSteerDelegate(func(ctx context.Context, input SteerDelegateInput) (SteerDelegateOutput, error) {
		if input.Boundary != SteerBoundaryAfterToolRoundtrip {
			return SteerDelegateOutput{}, nil
		}
		return SteerDelegateOutput{
			Events: []SteerEvent{{
				ID:         "steer_2",
				SourceType: "subagent",
				IntentType: "observation",
				Content:    "subagent result",
			}},
			RewriteRequest: &ContextBuildRequest{
				PrebuiltRequest: &core.CreateResponseRequest{
					Input: core.NewResponseInputItems([]core.ResponseInputItem{
						buildSystemMessageInputItem("[Runtime Steer]\nsubagent result"),
						buildUserMessageInputItem("start"),
					}),
				},
				PrebuiltHistoryInputItems: []core.ResponseInputItem{
					buildSystemMessageInputItem("[Runtime Steer]\nsubagent result"),
					buildUserMessageInputItem("start"),
				},
				PrebuiltAppliedHistoryMode: HistoryModeLocalReplay,
			},
			ForceHistoryMode:      HistoryModeLocalReplay,
			ResetPreviousResponse: true,
		}, nil
	})

	out, err := runner.RunWithContextResult(context.Background(), ContextBuildRequest{
		Inbound: InboundMessage{Role: "user", Content: "start"},
	})

	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done with steer" {
		t.Fatalf("expected final text after steer, got %q", out.FinalText)
	}
	if len(client.requests[1].Input.Items) == 0 ||
		len(client.requests[1].Input.Items[0].Content) == 0 ||
		!strings.Contains(client.requests[1].Input.Items[0].Content[0].Text, "Runtime Steer") {
		t.Fatalf("expected runtime steer in second request, got %#v", client.requests[1].Input)
	}
}

func TestSteerDelegate_StopBeforeNextModelCall(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{
		{ID: "resp-1", ToolCalls: []core.ToolCall{{CallID: "call-1", Name: "echo", Arguments: "{}"}}},
	}}
	registry := core.NewToolRegistry[struct{}]()
	if err := registry.Register(runnerStateTool{}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	runner := NewLoopRunner(client, registry, LoopRunnerOptions{MaxIterations: 3})
	runner.RegisterSteerDelegate(func(ctx context.Context, input SteerDelegateInput) (SteerDelegateOutput, error) {
		if input.Boundary == SteerBoundaryAfterToolRoundtrip {
			return SteerDelegateOutput{Stop: true, Reason: "user_stop"}, nil
		}
		return SteerDelegateOutput{}, nil
	})

	out, err := runner.RunWithContextResult(context.Background(), ContextBuildRequest{
		Inbound: InboundMessage{Role: "user", Content: "start"},
	})

	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.StopReason != "user_stop" {
		t.Fatalf("expected stop reason user_stop, got %q", out.StopReason)
	}
	if len(client.requests) != 1 {
		t.Fatalf("expected 1 model request, got %d", len(client.requests))
	}
}
