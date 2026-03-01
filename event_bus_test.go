package agentloop

import (
	"context"
	"testing"

	core "github.com/flaboy/agentloop/core"
)

func TestLoopEventBus_SubscribePublish(t *testing.T) {
	bus := NewLoopEventBus()
	count := 0
	unsub := bus.Subscribe(func(event LoopEvent) {
		if event == nil {
			t.Fatalf("event is nil")
		}
		count++
	})
	bus.Publish(ModelRequestEvent{})
	if count != 1 {
		t.Fatalf("expected count=1, got %d", count)
	}
	unsub()
	bus.Publish(ModelResponseEvent{})
	if count != 1 {
		t.Fatalf("expected count unchanged after unsubscribe, got %d", count)
	}
}

func TestLoopRunner_PublishesEventsToBus(t *testing.T) {
	client := &hookTestClient{responses: []core.CreateResponseResult{{FinalText: "ok"}}}
	runner := NewLoopRunner(client, nil, LoopRunnerOptions{MaxIterations: 1})
	seen := 0
	unsub := runner.EventBus().Subscribe(func(event LoopEvent) {
		if event == nil {
			t.Fatal("event is nil")
		}
		seen++
	})
	defer unsub()

	out, err := runner.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out != "ok" {
		t.Fatalf("unexpected output: %q", out)
	}
	if seen == 0 {
		t.Fatal("expected bus to receive events")
	}
}
