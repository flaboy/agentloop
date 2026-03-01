package agentloop

import (
	"context"
	"strings"
	"testing"

	core "github.com/flaboy/agentloop/core"
)

func TestResponsesClient_ToSDKRequest_AppliesReasoningEffort(t *testing.T) {
	client := NewResponsesClient(OpenAIConfig{
		Model:           "gpt-5",
		UseResponsesAPI: true,
		ReasoningEffort: "high",
	}, nil)
	req := core.CreateResponseRequest{
		Input: core.NewResponseInputText("hi"),
	}
	params, err := client.toSDKRequest(req)
	if err != nil {
		t.Fatalf("toSDKRequest failed: %v", err)
	}
	if params.Reasoning.Effort != "high" {
		t.Fatalf("unexpected reasoning effort: %q", params.Reasoning.Effort)
	}
}

func TestResponsesClient_ToSDKRequest_RejectsInvalidReasoningEffort(t *testing.T) {
	client := NewResponsesClient(OpenAIConfig{
		Model:           "gpt-5",
		UseResponsesAPI: true,
		ReasoningEffort: "extreme",
	}, nil)
	req := core.CreateResponseRequest{
		Input: core.NewResponseInputText("hi"),
	}
	_, err := client.toSDKRequest(req)
	if err == nil || !strings.Contains(err.Error(), "invalid reasoning effort") {
		t.Fatalf("expected invalid reasoning effort error, got %v", err)
	}
}

func TestResponsesClient_RejectsWhenResponsesAPIDisabled(t *testing.T) {
	client := NewResponsesClient(OpenAIConfig{
		Model:           "gpt-5",
		UseResponsesAPI: false,
	}, nil)
	_, err := client.CreateResponse(context.Background(), core.CreateResponseRequest{
		Input: core.NewResponseInputText("hi"),
	})
	if err == nil || !strings.Contains(err.Error(), "responses api is disabled") {
		t.Fatalf("expected disabled responses api error, got %v", err)
	}
}
