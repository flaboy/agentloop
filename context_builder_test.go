package agentloop

import (
	"testing"
)

func TestDefaultContextBuilder_BuildStructuredRequest(t *testing.T) {
	builder := DefaultContextBuilder{}
	result, err := builder.Build(ContextBuildRequest{
		Inbound: InboundMessage{
			Role:    "user",
			Content: "hello",
		},
		SystemContextJSON: `{"task_id":"t1"}`,
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if len(result.HistoryInputItems) != 2 {
		t.Fatalf("expected 2 input items, got %d", len(result.HistoryInputItems))
	}
	if result.HistoryInputItems[0].Role != "system" {
		t.Fatalf("expected first item role=system, got %q", result.HistoryInputItems[0].Role)
	}
	if result.HistoryInputItems[1].Role != "user" {
		t.Fatalf("expected second item role=user, got %q", result.HistoryInputItems[1].Role)
	}
}

func TestBuildContextRequestFromPrompt_LegacySystemContext(t *testing.T) {
	prompt := "event_type: user_input\nuser_input:\nhello\n\nsystem_context_json:\n{\"task_id\":\"t1\"}\n\nevent_context_json:\n{}\n\nconversation_history:\n[user#1] hi"
	req, err := BuildContextRequestFromPrompt(prompt)
	if err != nil {
		t.Fatalf("build context request failed: %v", err)
	}
	if req.Inbound.Content == "" {
		t.Fatal("inbound content should not be empty")
	}
	if req.SystemContextJSON != `{"task_id":"t1"}` {
		t.Fatalf("unexpected system context json: %q", req.SystemContextJSON)
	}
	if req.Inbound.Content == prompt {
		t.Fatal("expected inbound content stripped of legacy system_context section")
	}
}
