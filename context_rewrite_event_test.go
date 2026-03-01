package agentloop

import (
	"context"
	"testing"

	core "github.com/flaboy/agentloop/core"
)

func TestContextRewriteEvent_OnlyWhenInputChanges(t *testing.T) {
	t.Run("changed", func(t *testing.T) {
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
		_, err := runner.RunStreamWithContextAndTools(
			context.Background(),
			ContextBuildRequest{Inbound: InboundMessage{Role: "user", Content: "hello"}},
			nil,
			func(event LoopEvent) {
				if rewrite, ok := event.(ContextRewriteEvent); ok && rewrite.HistoryItemsUpdated {
					rewriteSeen = true
				}
			},
		)
		if err != nil {
			t.Fatalf("run failed: %v", err)
		}
		if !rewriteSeen {
			t.Fatal("expected context rewrite event when input changes")
		}
	})

	t.Run("unchanged", func(t *testing.T) {
		client := &hookTestClient{responses: []core.CreateResponseResult{
			{ID: "resp-1", ToolCalls: []core.ToolCall{{CallID: "call-1", Name: "echo", Arguments: "{}"}}},
			{ID: "resp-2", FinalText: "done"},
		}}
		registry := core.NewToolRegistry[struct{}]()
		if err := registry.Register(runnerStateTool{}); err != nil {
			t.Fatalf("register tool failed: %v", err)
		}
		runner := NewLoopRunner(client, registry, LoopRunnerOptions{MaxIterations: 3})
		runner.RegisterHook(HookPointRoundtrip, func(ctx *HookContext, next NextFunc) error {
			if err := next(); err != nil {
				return err
			}
			req := core.CreateResponseRequest{
				Input: core.NewResponseInputItems([]core.ResponseInputItem{
					buildUserMessageInputItem("hello"),
				}),
			}
			ctx.Request = &req
			return nil
		})
		rewriteSeen := false
		_, err := runner.RunStreamWithContextAndTools(
			context.Background(),
			ContextBuildRequest{Inbound: InboundMessage{Role: "user", Content: "hello"}},
			nil,
			func(event LoopEvent) {
				if _, ok := event.(ContextRewriteEvent); ok {
					rewriteSeen = true
				}
			},
		)
		if err != nil {
			t.Fatalf("run failed: %v", err)
		}
		if rewriteSeen {
			t.Fatal("did not expect context rewrite event when input is unchanged")
		}
	})
}
