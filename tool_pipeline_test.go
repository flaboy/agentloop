package agentloop

import (
	"context"
	"strings"
	"testing"

	core "github.com/flaboy/agentloop/core"
)

type toolPipelineEcho struct{}

func (toolPipelineEcho) Name() string { return "echo" }

func (toolPipelineEcho) Spec() core.ResponseToolSpec {
	return core.ResponseToolSpec{Type: "function", Name: "echo"}
}

func (toolPipelineEcho) Execute(_ context.Context, _ struct{}, input string, _ string) (string, *core.ToolError) {
	return input, nil
}

func (toolPipelineEcho) Cancel(_ context.Context, _ struct{}, _ string, _ string) *core.ToolError {
	return nil
}

func TestToolPipeline_NormalizesErrorWithErrorAndSuggest(t *testing.T) {
	pipeline := NewToolPipeline(nil)
	_, toolErr := pipeline.Execute(context.Background(), ToolPipelineInput{
		ToolCall: core.ToolCall{Name: "missing", Arguments: `{}`},
	})
	if toolErr == nil {
		t.Fatal("expected tool error")
	}
	if strings.TrimSpace(toolErr.ErrorString) == "" || strings.TrimSpace(toolErr.SuggestString) == "" {
		t.Fatal("error/suggest must both be non-empty")
	}
}

func TestToolPipeline_RejectsDisallowedTool(t *testing.T) {
	registry := core.NewToolRegistry[struct{}]()
	if err := registry.Register(toolPipelineEcho{}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	pipeline := NewToolPipeline(registry)
	_, toolErr := pipeline.Execute(context.Background(), ToolPipelineInput{
		AllowedTools:        map[string]struct{}{"other": {}},
		AllowlistConfigured: true,
		ToolCall:            core.ToolCall{Name: "echo", Arguments: `{"k":"v"}`},
	})
	if toolErr == nil {
		t.Fatal("expected allowlist rejection error")
	}
	if toolErr.ErrorString != "TOOL_NOT_ENABLED_IN_MODE" {
		t.Fatalf("unexpected error string: %q", toolErr.ErrorString)
	}
}

func TestToolPipeline_ExecutesAllowedTool(t *testing.T) {
	registry := core.NewToolRegistry[struct{}]()
	if err := registry.Register(toolPipelineEcho{}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	pipeline := NewToolPipeline(registry)
	out, toolErr := pipeline.Execute(context.Background(), ToolPipelineInput{
		AllowedTools:        map[string]struct{}{"echo": {}},
		AllowlistConfigured: true,
		ToolCall:            core.ToolCall{Name: "echo", Arguments: `{"ok":true}`},
	})
	if toolErr != nil {
		t.Fatalf("unexpected tool error: %v", toolErr)
	}
	if out != `{"ok":true}` {
		t.Fatalf("unexpected output: %q", out)
	}
}
