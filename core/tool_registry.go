package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

type ToolRegistry[S any] struct {
	mu     sync.RWMutex
	byName map[string]Tool[S]
}

func NewToolRegistry[S any]() *ToolRegistry[S] {
	return &ToolRegistry[S]{byName: map[string]Tool[S]{}}
}

func (r *ToolRegistry[S]) Register(tool Tool[S]) error {
	if r == nil {
		return errors.New("registry is nil")
	}
	if tool == nil {
		return errors.New("tool is nil")
	}
	name := strings.TrimSpace(tool.Name())
	if name == "" {
		return errors.New("tool name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byName[name]; exists {
		return fmt.Errorf("tool %q already registered", name)
	}
	r.byName[name] = tool
	return nil
}

func (r *ToolRegistry[S]) Get(name string) (Tool[S], bool) {
	var zero Tool[S]
	if r == nil {
		return zero, false
	}
	name = strings.TrimSpace(name)
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.byName[name]
	return tool, ok
}

func (r *ToolRegistry[S]) Specs() []ResponseToolSpec {
	if r == nil {
		return []ResponseToolSpec{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.byName))
	for name := range r.byName {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]ResponseToolSpec, 0, len(names))
	for _, name := range names {
		out = append(out, r.byName[name].Spec())
	}
	return out
}

func (r *ToolRegistry[S]) SpecsByNames(names []string) []ResponseToolSpec {
	if r == nil {
		return []ResponseToolSpec{}
	}
	allow := map[string]struct{}{}
	for _, item := range names {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		allow[name] = struct{}{}
	}
	if len(allow) == 0 {
		return []ResponseToolSpec{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(allow))
	for name := range allow {
		if _, ok := r.byName[name]; ok {
			keys = append(keys, name)
		}
	}
	slices.Sort(keys)
	out := make([]ResponseToolSpec, 0, len(keys))
	for _, name := range keys {
		out = append(out, r.byName[name].Spec())
	}
	return out
}

func (r *ToolRegistry[S]) Execute(ctx context.Context, state S, name string, input string, callID string) (string, *ToolError) {
	tool, ok := r.Get(name)
	if !ok {
		return "", NewToolError("TOOL_NOT_FOUND", "tool is not registered")
	}
	out, err := tool.Execute(ctx, state, input, callID)
	if err != nil {
		return "", err
	}
	return out, nil
}
