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

func TestParseResponseResult_PreservesTextAndToolCallsFromMixedResponse(t *testing.T) {
	raw := []byte(`{
		"id": "resp-1",
		"status": "completed",
		"output": [
			{
				"type": "message",
				"role": "assistant",
				"content": [
					{
						"type": "output_text",
						"text": "I'll read the skill document first."
					}
				]
			},
			{
				"type": "function_call",
				"id": "fc-1",
				"call_id": "call-1",
				"name": "echo",
				"arguments": "{}"
			}
		]
	}`)

	res, err := parseResponseResult(raw)
	if err != nil {
		t.Fatalf("parseResponseResult failed: %v", err)
	}
	if res.ID != "resp-1" {
		t.Fatalf("unexpected response id: %q", res.ID)
	}
	if res.FinalText != "I'll read the skill document first." {
		t.Fatalf("unexpected final text: %q", res.FinalText)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(res.ToolCalls))
	}
	if res.ToolCalls[0].CallID != "call-1" {
		t.Fatalf("unexpected call id: %#v", res.ToolCalls[0])
	}
	if res.ToolCalls[0].Name != "echo" {
		t.Fatalf("unexpected tool name: %#v", res.ToolCalls[0])
	}
}
