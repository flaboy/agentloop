package agentloop

import (
	"context"
	"sync"
	"testing"
	"time"

	core "github.com/flaboy/agentloop/core"
)

type blockingParallelTool struct {
	name       string
	started    chan string
	release    <-chan struct{}
	cancelMu   *sync.Mutex
	cancelled  *[]string
	executeOut string
}

func (t blockingParallelTool) Name() string { return t.name }

func (t blockingParallelTool) Spec() core.ResponseToolSpec {
	return core.ResponseToolSpec{Type: "function", Name: t.name}
}

func (t blockingParallelTool) Execute(ctx context.Context, _ struct{}, _ string, callID string) (string, *core.ToolError) {
	t.started <- callID
	select {
	case <-t.release:
		return t.executeOut, nil
	case <-ctx.Done():
		return "", core.NewToolError("TOOL_CONTEXT_CANCELLED", ctx.Err().Error())
	}
}

func (t blockingParallelTool) Cancel(_ context.Context, _ struct{}, _ string, callID string) *core.ToolError {
	t.cancelMu.Lock()
	defer t.cancelMu.Unlock()
	*t.cancelled = append(*t.cancelled, callID)
	return nil
}

func TestLoopRunner_ExecutesSameRoundToolCallsInParallel(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{
		{
			ID: "resp-1",
			ToolCalls: []core.ToolCall{
				{CallID: "call_a", Name: "tool_a", Arguments: `{}`},
				{CallID: "call_b", Name: "tool_b", Arguments: `{}`},
			},
		},
		{ID: "resp-2", FinalText: "done"},
	}}
	started := make(chan string, 2)
	release := make(chan struct{})
	var cancelled []string
	var cancelMu sync.Mutex
	registry := core.NewToolRegistry[struct{}]()
	for _, tool := range []blockingParallelTool{
		{name: "tool_a", started: started, release: release, cancelMu: &cancelMu, cancelled: &cancelled, executeOut: `{"tool":"a"}`},
		{name: "tool_b", started: started, release: release, cancelMu: &cancelMu, cancelled: &cancelled, executeOut: `{"tool":"b"}`},
	} {
		if err := registry.Register(tool); err != nil {
			t.Fatalf("register tool failed: %v", err)
		}
	}
	runner := NewLoopRunner(client, registry, LoopRunnerOptions{MaxIterations: 3})
	runDone := make(chan error, 1)
	go func() {
		_, err := runner.RunWithContext(context.Background(), ContextBuildRequest{
			Inbound: InboundMessage{Role: "user", Content: "hello"},
		})
		runDone <- err
	}()

	requireStartedCall(t, started, "call_a", "call_b")
	close(release)
	if err := <-runDone; err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("expected second model request after tool roundtrip, got %d", len(client.requests))
	}
	outputs := functionCallOutputs(client.requests[1].Input.Items)
	if len(outputs) != 2 {
		t.Fatalf("expected two function_call_output items, got %#v", outputs)
	}
	if got := outputs[0].Output; got != `{"tool":"a"}` {
		t.Fatalf("expected first output to stay in model order, got %q", got)
	}
	if got := outputs[1].Output; got != `{"tool":"b"}` {
		t.Fatalf("expected second output to stay in model order, got %q", got)
	}
}

func TestLoopRunner_CancelsRunningToolCallsWhenContextIsCancelled(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{{
		ID: "resp-1",
		ToolCalls: []core.ToolCall{
			{CallID: "call_a", Name: "tool_a", Arguments: `{}`},
			{CallID: "call_b", Name: "tool_b", Arguments: `{}`},
		},
	}}}
	started := make(chan string, 2)
	release := make(chan struct{})
	var cancelled []string
	var cancelMu sync.Mutex
	registry := core.NewToolRegistry[struct{}]()
	for _, tool := range []blockingParallelTool{
		{name: "tool_a", started: started, release: release, cancelMu: &cancelMu, cancelled: &cancelled},
		{name: "tool_b", started: started, release: release, cancelMu: &cancelMu, cancelled: &cancelled},
	} {
		if err := registry.Register(tool); err != nil {
			t.Fatalf("register tool failed: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner := NewLoopRunner(client, registry, LoopRunnerOptions{MaxIterations: 3})
	runDone := make(chan error, 1)
	go func() {
		_, err := runner.RunWithContext(ctx, ContextBuildRequest{
			Inbound: InboundMessage{Role: "user", Content: "hello"},
		})
		runDone <- err
	}()

	requireStartedCall(t, started, "call_a", "call_b")
	cancel()
	err := <-runDone
	if err == nil {
		t.Fatal("expected run to fail after context cancellation")
	}
	cancelMu.Lock()
	gotCancelled := append([]string(nil), cancelled...)
	cancelMu.Unlock()
	if !sameStringSet(gotCancelled, []string{"call_a", "call_b"}) {
		t.Fatalf("expected both running calls to be cancelled, got %#v", gotCancelled)
	}
	close(release)
}

func requireStartedCall(t *testing.T, started <-chan string, expected ...string) {
	t.Helper()
	got := make([]string, 0, len(expected))
	timeout := time.After(500 * time.Millisecond)
	for len(got) < len(expected) {
		select {
		case callID := <-started:
			got = append(got, callID)
		case <-timeout:
			t.Fatalf("timed out waiting for started calls, got %#v want %#v", got, expected)
		}
	}
	if !sameStringSet(got, expected) {
		t.Fatalf("unexpected started calls: got %#v want %#v", got, expected)
	}
}

func sameStringSet(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := map[string]int{}
	for _, item := range a {
		counts[item]++
	}
	for _, item := range b {
		counts[item]--
		if counts[item] < 0 {
			return false
		}
	}
	return true
}

func functionCallOutputs(items []core.ResponseInputItem) []core.ResponseInputItem {
	out := make([]core.ResponseInputItem, 0, len(items))
	for _, item := range items {
		if item.Type == "function_call_output" {
			out = append(out, item)
		}
	}
	return out
}
