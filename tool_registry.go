package agentloop

import (
	"context"
	"encoding/json"

	core "github.com/flaboy/agentloop/core"
)

type registryState struct{}

type toolAdapter struct {
	tool Tool
}

func (a toolAdapter) Name() string {
	return a.tool.Name()
}

func (a toolAdapter) Spec() core.ResponseToolSpec {
	return core.ResponseToolSpec(a.tool.Spec())
}

func (a toolAdapter) Execute(ctx context.Context, _ registryState, input json.RawMessage, callID string) (string, *core.ToolError) {
	return a.tool.Execute(ctx, input, callID)
}

type ToolRegistry struct {
	inner  *core.ToolRegistry[registryState]
	byName map[string]Tool
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		inner:  core.NewToolRegistry[registryState](),
		byName: map[string]Tool{},
	}
}

func (r *ToolRegistry) Register(tool Tool) error {
	if err := r.inner.Register(toolAdapter{tool: tool}); err != nil {
		return err
	}
	r.byName[tool.Name()] = tool
	return nil
}

func (r *ToolRegistry) Get(name string) (Tool, bool) {
	tool, ok := r.byName[name]
	return tool, ok
}

func (r *ToolRegistry) Specs() []ResponseToolSpec {
	return r.inner.Specs()
}

func (r *ToolRegistry) SpecsByNames(names []string) []ResponseToolSpec {
	return r.inner.SpecsByNames(names)
}

func (r *ToolRegistry) Execute(ctx context.Context, name string, input json.RawMessage, callID string) (string, *ToolError) {
	return r.inner.Execute(ctx, registryState{}, name, input, callID)
}
