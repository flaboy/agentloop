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

func TestParseResponseResultExtractsUsage(t *testing.T) {
	raw := []byte(`{
		"id":"resp_usage_1",
		"status":"completed",
		"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],
		"usage":{
			"input_tokens":11,
			"output_tokens":7,
			"total_tokens":18,
			"input_tokens_details":{"cached_tokens":3},
			"output_tokens_details":{"reasoning_tokens":2}
		}
	}`)

	result, err := parseResponseResult(raw)

	if err != nil {
		t.Fatalf("parseResponseResult failed: %v", err)
	}
	if result.Usage == nil {
		t.Fatalf("expected usage")
	}
	if result.Usage.InputTokens != 11 || result.Usage.OutputTokens != 7 || result.Usage.TotalTokens != 18 {
		t.Fatalf("unexpected usage: %+v", result.Usage)
	}
	if result.Usage.CachedTokens != 3 || result.Usage.ReasoningTokens != 2 {
		t.Fatalf("unexpected usage details: %+v", result.Usage)
	}
	if !strings.Contains(result.Usage.RawJSON, `"input_tokens":11`) {
		t.Fatalf("expected raw usage json, got %q", result.Usage.RawJSON)
	}
	if !strings.Contains(result.RawJSON, `"resp_usage_1"`) {
		t.Fatalf("expected raw response json, got %q", result.RawJSON)
	}
}

func TestParseResponseUsageSupportsTopLevelCachedAndReasoningTokens(t *testing.T) {
	raw := []byte(`{
		"id":"resp_usage_2",
		"usage":{
			"input_tokens":100,
			"output_tokens":20,
			"total_tokens":120,
			"cached_tokens":9,
			"reasoning_tokens":4
		}
	}`)

	usage := parseResponseUsage(raw)

	if usage == nil {
		t.Fatalf("expected usage")
	}
	if usage.CachedTokens != 9 || usage.ReasoningTokens != 4 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}
