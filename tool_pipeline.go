package agentloop

import (
	"context"
	"fmt"
	"strings"

	core "github.com/flaboy/agentloop/core"
)

type ToolPipelineInput struct {
	AllowedTools        map[string]struct{}
	AllowlistConfigured bool
	ToolCall            core.ToolCall
}

type ToolPipeline struct {
	registry *core.ToolRegistry[struct{}]
}

func NewToolPipeline(registry *core.ToolRegistry[struct{}]) *ToolPipeline {
	return &ToolPipeline{registry: registry}
}

func (p *ToolPipeline) Execute(ctx context.Context, in ToolPipelineInput) (string, *ToolError) {
	if p == nil || p.registry == nil {
		return "", NewToolError("TOOL_REGISTRY_UNAVAILABLE", "Ensure tool registry is initialized and injected into LoopRunner")
	}
	toolName := strings.TrimSpace(in.ToolCall.Name)
	if in.AllowlistConfigured {
		if _, ok := in.AllowedTools[toolName]; !ok {
			return "", NewToolError(
				"TOOL_NOT_ENABLED_IN_MODE",
				fmt.Sprintf("Tool %q is not in current allowed_tools; switch mode or adjust allowlist, then retry", toolName),
			)
		}
	}
	out, err := p.registry.Execute(ctx, struct{}{}, in.ToolCall.Name, in.ToolCall.Arguments, in.ToolCall.CallID)
	if err != nil {
		return "", err
	}
	return out, nil
}

func (p *ToolPipeline) Cancel(ctx context.Context, in ToolPipelineInput) *ToolError {
	if p == nil || p.registry == nil {
		return NewToolError("TOOL_REGISTRY_UNAVAILABLE", "Ensure tool registry is initialized and injected into LoopRunner")
	}
	toolName := strings.TrimSpace(in.ToolCall.Name)
	if in.AllowlistConfigured {
		if _, ok := in.AllowedTools[toolName]; !ok {
			return NewToolError(
				"TOOL_NOT_ENABLED_IN_MODE",
				fmt.Sprintf("Tool %q is not in current allowed_tools; switch mode or adjust allowlist, then retry", toolName),
			)
		}
	}
	return p.registry.Cancel(ctx, struct{}{}, in.ToolCall.Name, in.ToolCall.Arguments, in.ToolCall.CallID)
}
