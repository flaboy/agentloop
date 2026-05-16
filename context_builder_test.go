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

func TestDefaultContextBuilder_HybridAutoFallsBackToLocalReplayWithoutPreviousResponseID(t *testing.T) {
	builder := DefaultContextBuilder{}
	store := true

	result, err := builder.Build(ContextBuildRequest{
		Inbound:             InboundMessage{Role: "user", Content: "current turn"},
		SystemContextJSON:   `{"task_id":"t1"}`,
		ConversationHistory: "[user#1] hello\n[assistant#2] hi",
		HistoryMode:         HistoryModeHybridAuto,
		Store:               &store,
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if result.Request.PreviousResponseID != "" {
		t.Fatalf("expected empty previous_response_id, got %q", result.Request.PreviousResponseID)
	}
	if len(result.HistoryInputItems) != 3 {
		t.Fatalf("expected system + history + user items, got %d", len(result.HistoryInputItems))
	}
	if result.HistoryInputItems[1].Role != "system" {
		t.Fatalf("expected history block to be injected as system item, got %#v", result.HistoryInputItems[1])
	}
}

func TestDefaultContextBuilder_HybridAutoUsesProviderStateWhenPreviousResponseIDExists(t *testing.T) {
	builder := DefaultContextBuilder{}
	store := true

	result, err := builder.Build(ContextBuildRequest{
		Inbound:            InboundMessage{Role: "user", Content: "next turn"},
		SystemContextJSON:  `{"task_id":"t1"}`,
		HistoryMode:        HistoryModeHybridAuto,
		PreviousResponseID: "resp_prev_123",
		Store:              &store,
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if result.Request.PreviousResponseID != "resp_prev_123" {
		t.Fatalf("expected previous_response_id to be preserved, got %#v", result.Request)
	}
	if result.Request.Store == nil || !*result.Request.Store {
		t.Fatalf("expected store=true to be preserved, got %#v", result.Request.Store)
	}
	if len(result.HistoryInputItems) != 2 {
		t.Fatalf("expected system + current user only, got %d items", len(result.HistoryInputItems))
	}
}

func TestDefaultContextBuilder_ProviderStateAllowsEmptyInitialPreviousResponseID(t *testing.T) {
	store := true
	result, err := DefaultContextBuilder{}.Build(ContextBuildRequest{
		Inbound:     InboundMessage{Role: "user", Content: "hello"},
		HistoryMode: HistoryModeProviderState,
		Store:       &store,
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if result.AppliedHistoryMode != HistoryModeProviderState {
		t.Fatalf("expected provider_state, got %q", result.AppliedHistoryMode)
	}
	if result.Request.PreviousResponseID != "" {
		t.Fatalf("first provider_state request should not invent previous_response_id, got %q", result.Request.PreviousResponseID)
	}
	if len(result.Request.Input.Items) != 1 {
		t.Fatalf("expected inbound input only, got %#v", result.Request.Input.Items)
	}
}
