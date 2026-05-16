package core

import (
	"context"
	"testing"
)

func TestAPISurface_Compile(t *testing.T) {
	type demoState struct {
		ID string
	}

	var resolver PolicyResolver[demoState]
	if resolver != nil {
		t.Fatalf("expected nil resolver in compile contract check")
	}

	var tool Tool[demoState]
	if tool != nil {
		t.Fatalf("expected nil tool in compile contract check")
	}
}

type apiContractTool struct{}

func (apiContractTool) Name() string { return "contract" }

func (apiContractTool) Spec() ResponseToolSpec {
	return ResponseToolSpec{Type: "function", Name: "contract"}
}

func (apiContractTool) Execute(_ context.Context, _ struct{}, _ string, _ string) (string, *ToolError) {
	return `{"ok":true}`, nil
}

func (apiContractTool) Cancel(_ context.Context, _ struct{}, _ string, _ string) *ToolError {
	return nil
}

func TestToolRegistry_CancelUsesCoreToolContract(t *testing.T) {
	registry := NewToolRegistry[struct{}]()
	if err := registry.Register(apiContractTool{}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	if err := registry.Cancel(context.Background(), struct{}{}, "contract", `{}`, "call_1"); err != nil {
		t.Fatalf("cancel failed: %v", err)
	}
}
