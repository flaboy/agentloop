package agentloop

import (
	"context"
	"testing"

	core "github.com/flaboy/agentloop/core"
)

func TestLoopRunner_ZeroMaxIterationsAllowsMoreThanEightRounds(t *testing.T) {
	responses := make([]core.CreateResponseResult, 0, 10)
	for i := 0; i < 9; i++ {
		responses = append(responses, core.CreateResponseResult{
			ID: "resp-tool",
			ToolCalls: []core.ToolCall{{
				CallID:    "call-echo",
				Name:      "echo",
				Arguments: "{}",
			}},
		})
	}
	responses = append(responses, core.CreateResponseResult{
		ID:        "resp-final",
		FinalText: "done",
	})

	client := &hookTestClient{responses: responses}
	registry := core.NewToolRegistry[struct{}]()
	if err := registry.Register(runnerStateTool{}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}

	runner := NewLoopRunner(client, registry, LoopRunnerOptions{})
	out, err := runner.RunWithContext(context.Background(), ContextBuildRequest{
		Inbound: InboundMessage{Role: "user", Content: "hello"},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out != "done" {
		t.Fatalf("unexpected output: %q", out)
	}
	if len(client.requests) != 10 {
		t.Fatalf("expected 10 model requests, got %d", len(client.requests))
	}
}
