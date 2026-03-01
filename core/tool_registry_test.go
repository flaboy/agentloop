package core

import (
	"context"
	"reflect"
	"testing"
)

type registryTestState struct {
	Project string
}

type registryTestTool struct {
	name string
}

func (t registryTestTool) Name() string { return t.name }

func (t registryTestTool) Spec() ResponseToolSpec {
	return ResponseToolSpec{Type: "function", Name: t.name}
}

func (t registryTestTool) Execute(_ context.Context, _ registryTestState, _ string, _ string) (string, *ToolError) {
	return "ok", nil
}

func TestToolRegistry_RegisterAndSpecsByNames(t *testing.T) {
	r := NewToolRegistry[registryTestState]()
	if err := r.Register(registryTestTool{name: "b"}); err != nil {
		t.Fatalf("register b failed: %v", err)
	}
	if err := r.Register(registryTestTool{name: "a"}); err != nil {
		t.Fatalf("register a failed: %v", err)
	}

	filtered := r.SpecsByNames([]string{"b"})
	if len(filtered) != 1 || filtered[0].Name != "b" {
		t.Fatalf("unexpected filtered specs: %#v", filtered)
	}

	all := r.Specs()
	got := []string{all[0].Name, all[1].Name}
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected spec order: got=%v want=%v", got, want)
	}
}

func TestToolRegistry_RejectDuplicate(t *testing.T) {
	r := NewToolRegistry[registryTestState]()
	if err := r.Register(registryTestTool{name: "dup"}); err != nil {
		t.Fatalf("register first failed: %v", err)
	}
	if err := r.Register(registryTestTool{name: "dup"}); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}
