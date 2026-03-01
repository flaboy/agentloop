package agentloop

import (
	"context"
	"fmt"

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
}

type HookFunc func(ctx *HookContext, next NextFunc) error

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
