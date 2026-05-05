package agentloop

import (
	"context"
	"testing"

	core "github.com/flaboy/agentloop/core"
)

type runnerStateTool struct{}

func (runnerStateTool) Name() string { return "echo" }

func (runnerStateTool) Spec() core.ResponseToolSpec {
	return core.ResponseToolSpec{Type: "function", Name: "echo"}
}

func (runnerStateTool) Execute(_ context.Context, _ struct{}, _ string, _ string) (string, *core.ToolError) {
	return `{"ok":true}`, nil
}

func TestLoopRunner_RecordsTransitions(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{{FinalText: "done"}}}
	runner := NewLoopRunner(client, nil, LoopRunnerOptions{MaxIterations: 2})
	out, err := runner.RunWithContext(context.Background(), ContextBuildRequest{
		Inbound: InboundMessage{Role: "user", Content: "hello"},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out != "done" {
		t.Fatalf("unexpected output: %q", out)
	}
	records := runner.LastTransitions()
	if len(records) == 0 {
		t.Fatal("expected transitions to be recorded")
	}
	if records[0].Event != RunnerEventRunStarted || records[0].To != RunnerStatePreparingContext {
		t.Fatalf("unexpected first transition: %#v", records[0])
	}
	last := records[len(records)-1]
	if last.Event != RunnerEventRunCompleted || last.To != RunnerStateCompleted {
		t.Fatalf("unexpected last transition: %#v", last)
	}

	guard := NewRunnerTransitionGuard()
	for _, record := range records {
		if err := guard.Validate(record.From, record.Event, record.To); err != nil {
			t.Fatalf("invalid transition record: %+v err=%v", record, err)
		}
	}
}

func TestLoopRunner_EmitsContextRewriteEvent(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{
		{ID: "resp-1", ToolCalls: []core.ToolCall{{CallID: "call-1", Name: "echo", Arguments: "{}"}}},
		{ID: "resp-2", FinalText: "done"},
	}}
	registry := core.NewToolRegistry[struct{}]()
	if err := registry.Register(runnerStateTool{}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	runner := NewLoopRunner(client, registry, LoopRunnerOptions{MaxIterations: 3})
	rewriteSeen := false
	transitionRewriteSeen := false
	out, err := runner.RunStreamWithContextAndTools(
		context.Background(),
		ContextBuildRequest{Inbound: InboundMessage{Role: "user", Content: "hello"}},
		nil,
		func(event LoopEvent) {
			switch e := event.(type) {
			case ContextRewriteEvent:
				if e.HistoryItemsUpdated {
					rewriteSeen = true
				}
			case TransitionEvent:
				if e.Record.Event == RunnerEventContextRewritten {
					transitionRewriteSeen = true
				}
			}
		},
	)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out != "done" {
		t.Fatalf("unexpected output: %q", out)
	}
	if !rewriteSeen {
		t.Fatal("expected context rewrite event to be emitted")
	}
	if !transitionRewriteSeen {
		t.Fatal("expected context rewrite transition to be emitted")
	}
}

func TestLoopRunner_RunWithContextResultReturnsFinalResponseID(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{{ID: "resp-final-1", FinalText: "done"}}}
	runner := NewLoopRunner(client, nil, LoopRunnerOptions{MaxIterations: 2})

	out, err := runner.RunWithContextResult(context.Background(), ContextBuildRequest{
		Inbound:            InboundMessage{Role: "user", Content: "hello"},
		HistoryMode:        HistoryModeProviderState,
		PreviousResponseID: "resp-prev",
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected output: %#v", out)
	}
	if out.FinalResponseID != "resp-final-1" {
		t.Fatalf("expected final response id, got %#v", out)
	}
}

func TestLoopRunner_RunStreamWithContextResultReturnsFinalResponseID(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{{ID: "resp-final-stream-1", FinalText: "done"}}}
	runner := NewLoopRunner(client, nil, LoopRunnerOptions{MaxIterations: 2})

	out, err := runner.RunStreamWithContextResult(context.Background(), ContextBuildRequest{
		Inbound:     InboundMessage{Role: "user", Content: "hello"},
		HistoryMode: HistoryModeLocalReplay,
	}, nil)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected output: %#v", out)
	}
	if out.FinalResponseID != "resp-final-stream-1" {
		t.Fatalf("expected final response id, got %#v", out)
	}
}

func TestLoopRunner_ProviderStateRoundtripUsesPreviousResponseIDInsteadOfReplayHistory(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{
		{ID: "resp-1", ToolCalls: []core.ToolCall{{CallID: "call-1", Name: "echo", Arguments: "{}"}}},
		{ID: "resp-2", FinalText: "done"},
	}}
	registry := core.NewToolRegistry[struct{}]()
	if err := registry.Register(runnerStateTool{}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	runner := NewLoopRunner(client, registry, LoopRunnerOptions{MaxIterations: 3})

	out, err := runner.RunWithContextResult(context.Background(), ContextBuildRequest{
		Inbound:            InboundMessage{Role: "user", Content: "hello"},
		HistoryMode:        HistoryModeProviderState,
		PreviousResponseID: "resp-bootstrap",
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalResponseID != "resp-2" {
		t.Fatalf("expected final response id resp-2, got %#v", out)
	}
	if len(client.requests) < 2 {
		t.Fatalf("expected two model requests, got %d", len(client.requests))
	}
	second := client.requests[1]
	if second.PreviousResponseID != "resp-1" {
		t.Fatalf("expected second request to chain response id, got %#v", second)
	}
	if len(second.Input.Items) != 1 {
		t.Fatalf("expected only function_call_output in provider-state roundtrip, got %#v", second.Input.Items)
	}
	if second.Input.Items[0].Type != "function_call_output" {
		t.Fatalf("expected function_call_output, got %#v", second.Input.Items)
	}
}

func TestLoopRunner_MixedFinalTextAndToolCallsExecutesTools(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{
		{
			ID:        "resp-1",
			FinalText: "premature text",
			ToolCalls: []core.ToolCall{{CallID: "call-1", Name: "echo", Arguments: "{}"}},
		},
		{ID: "resp-2", FinalText: "done"},
	}}
	registry := core.NewToolRegistry[struct{}]()
	if err := registry.Register(runnerStateTool{}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	runner := NewLoopRunner(client, registry, LoopRunnerOptions{MaxIterations: 3})

	out, err := runner.RunWithContext(context.Background(), ContextBuildRequest{
		Inbound: InboundMessage{Role: "user", Content: "hello"},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out != "done" {
		t.Fatalf("unexpected output: %q", out)
	}
	if len(client.requests) != 2 {
		t.Fatalf("expected tool roundtrip to trigger second request, got %d requests", len(client.requests))
	}
	second := client.requests[1]
	if len(second.Input.Items) != 3 {
		t.Fatalf("expected replayed history plus tool output, got %#v", second.Input.Items)
	}
	last := second.Input.Items[len(second.Input.Items)-1]
	if last.Type != "function_call_output" {
		t.Fatalf("expected function_call_output replay, got %#v", last)
	}
	if last.Output != `{"ok":true}` {
		t.Fatalf("unexpected tool output replay: %#v", last)
	}
}
