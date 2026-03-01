package responses

import (
	"testing"
)

func TestStreamParser_AggregatesToolCallsAndFinalText(t *testing.T) {
	acc := NewStreamAccumulator()
	events := []string{
		`{"type":"response.created","response":{"id":"resp_1"}}`,
		`{"type":"response.output_text.delta","delta":"hel"}`,
		`{"type":"response.output_text.delta","delta":"lo"}`,
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"echo","arguments":"{\"text\":\"hi\"}"}}`,
	}
	for _, ev := range events {
		if err := acc.ApplyEvent([]byte(ev)); err != nil {
			t.Fatalf("apply event failed: %v", err)
		}
	}
	res := acc.Result()
	if res.ID != "resp_1" {
		t.Fatalf("unexpected response id: %q", res.ID)
	}
	if res.FinalText != "hello" {
		t.Fatalf("unexpected final text: %q", res.FinalText)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("unexpected tool call count: %d", len(res.ToolCalls))
	}
	if res.ToolCalls[0].CallID != "call_1" || res.ToolCalls[0].Name != "echo" {
		t.Fatalf("unexpected tool call: %#v", res.ToolCalls[0])
	}
}
