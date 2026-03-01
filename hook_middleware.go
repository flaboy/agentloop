package agentloop

import (
	"context"
	"fmt"
	"strings"

	core "github.com/flaboy/agentloop/core"
)

type HookPoint string

const (
	HookPointModelCall HookPoint = "model_call"
	HookPointToolCall  HookPoint = "tool_call"
	HookPointRoundtrip HookPoint = "roundtrip"
)

type NextFunc func() error

type HookContext struct {
	Ctx             context.Context
	Iteration       int
	Request         *core.CreateResponseRequest
	Response        *core.CreateResponseResult
	ToolCall        *core.ToolCall
	ToolOutput      *string
	ToolErrorString *string
	allowedToolSet  map[string]struct{}
	allowlistSet    bool
}

type HookFunc func(ctx *HookContext, next NextFunc) error

func (c *HookContext) SetAllowedToolNames(names []string) {
	if c == nil {
		return
	}
	out := map[string]struct{}{}
	for _, item := range names {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	c.allowedToolSet = out
	c.allowlistSet = true
}

func (c *HookContext) AllowedToolNameSet() (map[string]struct{}, bool) {
	if c == nil || !c.allowlistSet {
		return nil, false
	}
	out := map[string]struct{}{}
	for name := range c.allowedToolSet {
		out[name] = struct{}{}
	}
	return out, true
}

func (r *LoopRunner) RegisterHook(point HookPoint, hook HookFunc) {
	if r == nil || hook == nil {
		return
	}
	r.hooksMu.Lock()
	defer r.hooksMu.Unlock()
	if r.hooks == nil {
		r.hooks = map[HookPoint][]HookFunc{}
	}
	r.hooks[point] = append(r.hooks[point], hook)
}

func (r *LoopRunner) runHookChain(point HookPoint, hookCtx *HookContext, terminal NextFunc) error {
	if terminal == nil {
		return nil
	}
	if hookCtx == nil {
		hookCtx = &HookContext{}
	}
	r.hooksMu.RLock()
	hooks := append([]HookFunc(nil), r.hooks[point]...)
	r.hooksMu.RUnlock()
	if len(hooks) == 0 {
		return terminal()
	}
	index := 0
	var chain NextFunc
	chain = func() error {
		if index >= len(hooks) {
			return terminal()
		}
		current := hooks[index]
		index++
		called := false
		err := current(hookCtx, func() error {
			called = true
			return chain()
		})
		if err != nil {
			return err
		}
		if !called {
			return fmt.Errorf("hook %q did not call next", string(point))
		}
		return nil
	}
	return chain()
}
