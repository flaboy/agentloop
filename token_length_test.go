package agentloop

import (
	"strings"
	"testing"

	core "github.com/flaboy/agentloop/core"
)

func TestApproximateRequestTokenLength_IncludesToolOutputs(t *testing.T) {
	req := core.CreateResponseRequest{
		Input: core.NewResponseInputItems([]core.ResponseInputItem{
			{
				Type: "message",
				Role: "user",
				Content: []core.ResponseInputContentPart{{
					Type: "input_text",
					Text: "hello",
				}},
			},
			{
				Type:   "function_call_output",
				CallID: "call_1",
				Output: strings.Repeat("x", 400),
			},
		}),
	}
	got := ApproximateRequestTokenLength(req)
	if got < 100 {
		t.Fatalf("expected tool output to be counted, got %d", got)
	}
}

func TestTokenLengthEstimatorOverride(t *testing.T) {
	runner := NewLoopRunner(nil, nil, LoopRunnerOptions{
		TokenLengthEstimator: func(core.CreateResponseRequest) int64 {
			return 123
		},
	})
	got := runner.estimateRequestTokenLength(core.CreateResponseRequest{})
	if got != 123 {
		t.Fatalf("expected override estimate, got %d", got)
	}
}
