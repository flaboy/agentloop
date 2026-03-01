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
