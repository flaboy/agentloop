package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	core "github.com/flaboy/agentloop/core"
)

type ResponsesAPI interface {
	CreateResponse(ctx context.Context, req core.CreateResponseRequest) (*core.CreateResponseResult, error)
}

type ResponsesStreamAPI interface {
	CreateResponseStream(ctx context.Context, req core.CreateResponseRequest, onTextDelta func(string)) (*core.CreateResponseResult, error)
}

type LoopRunnerOptions struct {
	MaxIterations  int
	ContextBuilder ContextBuilder
}

type LoopRunner struct {
	client         ResponsesAPI
	tools          *core.ToolRegistry[struct{}]
	options        LoopRunnerOptions
	contextBuilder ContextBuilder
	eventBus       *LoopEventBus
	hooksMu        sync.RWMutex
	hooks          map[HookPoint][]HookFunc
}

func NewLoopRunner(client ResponsesAPI, tools *core.ToolRegistry[struct{}], options LoopRunnerOptions) *LoopRunner {
	if options.MaxIterations <= 0 {
		options.MaxIterations = 8
	}
	builder := options.ContextBuilder
	if builder == nil {
		builder = DefaultContextBuilder{}
	}
	return &LoopRunner{
		client:         client,
		tools:          tools,
		options:        options,
		contextBuilder: builder,
		eventBus:       NewLoopEventBus(),
		hooks:          map[HookPoint][]HookFunc{},
	}
}

func (r *LoopRunner) EventBus() *LoopEventBus {
	if r == nil {
		return nil
	}
	return r.eventBus
}

func (r *LoopRunner) Run(ctx context.Context, userPrompt string) (string, error) {
	return r.run(ctx, userPrompt, nil, nil)
}

func (r *LoopRunner) RunStream(ctx context.Context, userPrompt string, onTextDelta func(string)) (string, error) {
	return r.run(ctx, userPrompt, onTextDelta, nil)
}

func (r *LoopRunner) RunStreamWithTools(
	ctx context.Context,
	userPrompt string,
	onTextDelta func(string),
	onToolEvent func(LoopEvent),
) (string, error) {
	return r.run(ctx, userPrompt, onTextDelta, onToolEvent)
}

func (r *LoopRunner) run(
	ctx context.Context,
	userPrompt string,
	onTextDelta func(string),
	onToolEvent func(LoopEvent),
) (string, error) {
	if r == nil || r.client == nil {
		return "", errors.New("loop runner client is required")
	}
	if r.contextBuilder == nil {
		return "", errors.New("context builder is required")
	}

	contextResult, buildErr := r.contextBuilder.Build(ContextBuildRequest{
		UserPrompt: userPrompt,
	})
	if buildErr != nil {
		return "", fmt.Errorf("build initial context failed: %w", buildErr)
	}
	req := contextResult.Request
	historyInputItems := cloneResponseInputItems(contextResult.HistoryInputItems)
	lastResponseTrace := ""

	for i := 0; i < r.options.MaxIterations; i++ {
		iteration := i + 1
		callReq := req
		if err := core.ValidateResponseInputInvariants(callReq.Input); err != nil {
			base := fmt.Sprintf("responses input invariant failed iteration=%d %s", iteration, summarizeCreateResponseRequest(callReq))
			return "", fmt.Errorf("%s: %w", base, err)
		}
		reqSummary := summarizeCreateResponseRequest(callReq)
		r.emitLoopEvent(onToolEvent, ModelRequestEvent{
			Iteration:        iteration,
			Timestamp:        time.Now(),
			Request:          reqSummary,
			PreviousResponse: lastResponseTrace,
			RoundtripMode:    roundtripModeName(true),
		})

		callCtx := ctx

		var (
			res *core.CreateResponseResult
			err error
		)
		modelHookCtx := &HookContext{
			Ctx:       callCtx,
			Iteration: iteration,
			Request:   &callReq,
		}
		err = r.runHookChain(HookPointModelCall, modelHookCtx, func() error {
			allowedTools, allowlistConfigured := modelHookCtx.AllowedToolNameSet()
			if r.tools != nil {
				modelHookCtx.Request.Tools = r.resolveToolSpecs(allowedTools, allowlistConfigured)
			}
			if onTextDelta != nil {
				if streamClient, ok := r.client.(ResponsesStreamAPI); ok {
					res, err = streamClient.CreateResponseStream(callCtx, *modelHookCtx.Request, onTextDelta)
				} else {
					res, err = r.client.CreateResponse(callCtx, *modelHookCtx.Request)
					if err == nil && res != nil && strings.TrimSpace(res.FinalText) != "" {
						onTextDelta(res.FinalText)
					}
				}
			} else {
				res, err = r.client.CreateResponse(callCtx, *modelHookCtx.Request)
			}
			modelHookCtx.Response = res
			return err
		})
		res = modelHookCtx.Response
		allowedTools, allowlistConfigured := modelHookCtx.AllowedToolNameSet()
		if err != nil {
			base := fmt.Sprintf("responses request failed iteration=%d %s", iteration, reqSummary)
			if strings.TrimSpace(lastResponseTrace) != "" {
				base += " prev_response_trace=" + lastResponseTrace
			}
			return "", fmt.Errorf("%s: %w", base, err)
		}
		if res == nil {
			return "", fmt.Errorf("responses request returned nil response iteration=%d %s", iteration, reqSummary)
		}

		currentTrace := summarizeEventTrace(res.EventTrace)
		r.emitLoopEvent(onToolEvent, ModelResponseEvent{
			Iteration:        iteration,
			Timestamp:        time.Now(),
			ResponseID:       strings.TrimSpace(res.ID),
			ToolCalls:        len(res.ToolCalls),
			ToolCallsSummary: summarizeToolCalls(res.ToolCalls),
			FinalTextLen:     len(strings.TrimSpace(res.FinalText)),
			EventTrace:       currentTrace,
			EventCount:       len(res.EventTrace),
		})
		lastResponseTrace = currentTrace

		if res.HasFinalText() {
			return res.FinalText, nil
		}
		if len(res.ToolCalls) == 0 {
			return "", fmt.Errorf(
				"responses api returned no output_text and no tool_calls iteration=%d response_id=%q %s response_trace=%s",
				iteration,
				strings.TrimSpace(res.ID),
				reqSummary,
				currentTrace,
			)
		}

		outputs := make([]core.ResponseInputItem, 0, len(res.ToolCalls))
		replayItems := make([]core.ResponseInputItem, 0, len(res.ToolCalls)*2)
		for _, call := range res.ToolCalls {
			callID := strings.TrimSpace(call.CallID)
			if callID == "" {
				return "", fmt.Errorf(
					"responses tool call missing call_id iteration=%d tool=%s id=%s response_id=%q %s",
					iteration,
					strings.TrimSpace(call.Name),
					strings.TrimSpace(call.ID),
					strings.TrimSpace(res.ID),
					reqSummary,
				)
			}

			r.emitLoopEvent(onToolEvent, ToolInputEvent{
				Iteration:    iteration,
				Timestamp:    time.Now(),
				CallID:       callID,
				ResponseID:   strings.TrimSpace(res.ID),
				ToolName:     strings.TrimSpace(call.Name),
				Input:        normalizeJSONText(call.Arguments),
				InputRawLen:  len(strings.TrimSpace(call.Arguments)),
				InputPreview: clipForLog(strings.TrimSpace(call.Arguments), 800),
			})

			out := ""
			outputErrText := ""
			outputIsError := false
			toolHookCtx := &HookContext{
				Ctx:       ctx,
				Iteration: iteration,
				Response:  res,
				ToolCall:  &call,
			}
			hookErr := r.runHookChain(HookPointToolCall, toolHookCtx, func() error {
				out, outputErrText, outputIsError = r.executeToolCall(ctx, allowedTools, allowlistConfigured, call)
				toolHookCtx.ToolOutput = &out
				if outputIsError {
					toolHookCtx.ToolErrorString = &outputErrText
				}
				return nil
			})
			if hookErr != nil {
				return "", fmt.Errorf("tool hook failed iteration=%d call_id=%q tool=%q: %w", iteration, callID, strings.TrimSpace(call.Name), hookErr)
			}
			if toolHookCtx.ToolOutput != nil {
				out = *toolHookCtx.ToolOutput
			}
			if toolHookCtx.ToolErrorString != nil {
				outputErrText = *toolHookCtx.ToolErrorString
				outputIsError = strings.TrimSpace(outputErrText) != ""
			}
			outputState := "output-available"
			if outputIsError {
				outputState = "output-error"
			}
			r.emitLoopEvent(onToolEvent, ToolOutputEvent{
				Iteration:   iteration,
				Timestamp:   time.Now(),
				CallID:      callID,
				ResponseID:  strings.TrimSpace(res.ID),
				ToolName:    strings.TrimSpace(call.Name),
				State:       outputState,
				ErrorString: outputErrText,
				OutputLen:   len(strings.TrimSpace(out)),
				Output:      normalizeJSONText(out),
			})

			replayCall := buildReplayFunctionCallInputItem(call)
			outputItem := core.ResponseInputItem{Type: "function_call_output", CallID: callID, Output: out}
			outputs = append(outputs, outputItem)
			replayItems = append(replayItems, replayCall, outputItem)
		}

		r.emitLoopEvent(onToolEvent, RoundtripPreparedEvent{
			Iteration:          iteration,
			Timestamp:          time.Now(),
			PreviousResponseID: strings.TrimSpace(res.ID),
			RoundtripMode:      roundtripModeName(true),
			ItemsCount:         len(outputs),
			ItemsSummary:       summarizeResponseInput(core.NewResponseInputItems(outputs)),
		})
		roundHookCtx := &HookContext{
			Ctx:       ctx,
			Iteration: iteration,
			Response:  res,
		}
		err = r.runHookChain(HookPointRoundtrip, roundHookCtx, func() error {
			historyInputItems = append(historyInputItems, replayItems...)
			req = core.CreateResponseRequest{
				Input: core.NewResponseInputItems(cloneResponseInputItems(historyInputItems)),
			}
			roundHookCtx.Request = &req
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("roundtrip hook failed iteration=%d: %w", iteration, err)
		}
		if roundHookCtx.Request != nil {
			req = *roundHookCtx.Request
		}
	}
	return "", fmt.Errorf("responses loop exceeded max iterations: %d", r.options.MaxIterations)
}

func (r *LoopRunner) executeToolCall(
	ctx context.Context,
	allowedTools map[string]struct{},
	allowlistConfigured bool,
	call core.ToolCall,
) (string, string, bool) {
	if r.tools == nil {
		err := NewToolError("TOOL_REGISTRY_UNAVAILABLE", "Ensure tool registry is initialized and injected into LoopRunner")
		return mustMarshalToolError(err), err.ErrorString, true
	}
	if allowlistConfigured {
		if _, ok := allowedTools[strings.TrimSpace(call.Name)]; !ok {
			err := NewToolError(
				"TOOL_NOT_ENABLED_IN_MODE",
				fmt.Sprintf("Tool %q is not in current allowed_tools; switch mode or adjust allowlist, then retry", strings.TrimSpace(call.Name)),
			)
			return mustMarshalToolError(err), err.ErrorString, true
		}
	}
	toolOut, err := r.tools.Execute(ctx, struct{}{}, call.Name, call.Arguments, call.CallID)
	if err != nil {
		return mustMarshalToolError(err), err.ErrorString, true
	}
	return toolOut, "", false
}

func (r *LoopRunner) emitLoopEvent(onToolEvent func(LoopEvent), event LoopEvent) {
	if r != nil && r.eventBus != nil {
		r.eventBus.Publish(event)
	}
	if onToolEvent == nil {
		return
	}
	onToolEvent(event)
}

func extractPromptSystemContextJSON(prompt string) (string, bool) {
	start, end, _, _, ok := extractPromptSystemAndEventSectionRanges(prompt)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(prompt[start:end]), true
}

func stripPromptSystemContextSection(prompt string) (string, bool) {
	_, _, systemSectionStart, eventSectionStart, ok := extractPromptSystemAndEventSectionRanges(prompt)
	if !ok {
		return prompt, false
	}
	next := prompt[:systemSectionStart] + prompt[eventSectionStart:]
	return strings.TrimSpace(next), true
}

func extractPromptSystemAndEventSectionRanges(prompt string) (systemJSONStart, systemJSONEnd, systemSectionStart, eventSectionStart int, ok bool) {
	const (
		systemMarker       = "\n\nsystem_context_json:"
		eventMarker        = "\n\nevent_context_json:"
		conversationMarker = "\n\nconversation_history:"
	)
	convIdx := strings.Index(prompt, conversationMarker)
	if convIdx < 0 {
		return 0, 0, 0, 0, false
	}
	eventIdx := strings.LastIndex(prompt[:convIdx], eventMarker)
	if eventIdx < 0 {
		return 0, 0, 0, 0, false
	}
	systemIdx := strings.LastIndex(prompt[:eventIdx], systemMarker)
	if systemIdx < 0 {
		return 0, 0, 0, 0, false
	}
	start := systemIdx + len(systemMarker)
	for start < len(prompt) {
		ch := prompt[start]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			start++
			continue
		}
		break
	}
	if start >= len(prompt) || prompt[start] != '{' {
		return 0, 0, 0, 0, false
	}
	end, found := findJSONObjectEnd(prompt, start)
	if !found || end > eventIdx {
		return 0, 0, 0, 0, false
	}
	return start, end, systemIdx, eventIdx, true
}

func extractPromptJSONObjectRange(prompt string, marker string) (int, int, bool) {
	idx := strings.Index(prompt, marker)
	if idx < 0 {
		return 0, 0, false
	}
	start := idx + len(marker)
	for start < len(prompt) {
		ch := prompt[start]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			start++
			continue
		}
		break
	}
	if start >= len(prompt) || prompt[start] != '{' {
		return 0, 0, false
	}
	end, ok := findJSONObjectEnd(prompt, start)
	if !ok {
		return 0, 0, false
	}
	return start, end, true
}

func findJSONObjectEnd(input string, start int) (int, bool) {
	if start < 0 || start >= len(input) || input[start] != '{' {
		return 0, false
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(input); i++ {
		ch := input[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}

func (r *LoopRunner) resolveToolSpecs(allowedTools map[string]struct{}, allowlistConfigured bool) []core.ResponseToolSpec {
	if r == nil || r.tools == nil {
		return nil
	}
	if !allowlistConfigured {
		return r.tools.Specs()
	}
	if len(allowedTools) == 0 {
		return []core.ResponseToolSpec{}
	}
	names := make([]string, 0, len(allowedTools))
	for name := range allowedTools {
		names = append(names, name)
	}
	return r.tools.SpecsByNames(names)
}

func normalizeJSONText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if json.Valid([]byte(trimmed)) {
		return trimmed
	}
	enc, _ := json.Marshal(trimmed)
	return string(enc)
}

func summarizeEventTrace(trace []string) string {
	if len(trace) == 0 {
		return ""
	}
	joined := strings.Join(trace, " | ")
	joined = strings.TrimSpace(joined)
	if len(joined) > 2000 {
		return joined[:2000] + "...(truncated)"
	}
	return joined
}

func clipForLog(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "...(truncated)"
}

func summarizeCreateResponseRequest(req core.CreateResponseRequest) string {
	storeSummary := "<unset>"
	if req.Store != nil {
		storeSummary = fmt.Sprintf("%t", *req.Store)
	}
	parts := []string{
		fmt.Sprintf("stream=%t", req.Stream),
		fmt.Sprintf("store=%s", storeSummary),
		fmt.Sprintf("previous_response_id=%q", strings.TrimSpace(req.PreviousResponseID)),
		fmt.Sprintf("tools=%d", len(req.Tools)),
		fmt.Sprintf("input=%s", summarizeResponseInput(req.Input)),
	}
	return strings.Join(parts, " ")
}

func roundtripModeName(fullContext bool) string {
	if fullContext {
		return "full_context"
	}
	return "previous_response_id"
}

func cloneResponseInputItems(in []core.ResponseInputItem) []core.ResponseInputItem {
	if len(in) == 0 {
		return []core.ResponseInputItem{}
	}
	out := make([]core.ResponseInputItem, len(in))
	copy(out, in)
	return out
}

func buildUserMessageInputItem(text string) core.ResponseInputItem {
	return core.ResponseInputItem{
		Type: "message",
		Role: "user",
		Content: []core.ResponseInputContentPart{{
			Type: "input_text",
			Text: strings.TrimSpace(text),
		}},
	}
}

func buildSystemMessageInputItem(text string) core.ResponseInputItem {
	return core.ResponseInputItem{
		Type: "message",
		Role: "system",
		Content: []core.ResponseInputContentPart{{
			Type: "input_text",
			Text: strings.TrimSpace(text),
		}},
	}
}

func buildReplayFunctionCallInputItem(call core.ToolCall) core.ResponseInputItem {
	callID := strings.TrimSpace(call.CallID)
	itemID := strings.TrimSpace(call.ID)
	if itemID == "" {
		itemID = callID
	}
	arguments := strings.TrimSpace(call.Arguments)
	if arguments == "" {
		arguments = "{}"
	}
	return core.ResponseInputItem{
		Type:      "function_call",
		ID:        itemID,
		CallID:    callID,
		Name:      sanitizeFunctionCallNameForInput(call.Name),
		Arguments: arguments,
	}
}

func sanitizeFunctionCallNameForInput(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "tool_call"
	}
	var b strings.Builder
	b.Grow(len(name))
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			b.WriteRune(ch)
			continue
		}
		b.WriteByte('_')
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "tool_call"
	}
	return out
}

func summarizeResponseInput(input core.ResponseInput) string {
	if strings.TrimSpace(input.Text) != "" {
		return fmt.Sprintf("text(len=%d)", len(strings.TrimSpace(input.Text)))
	}
	if len(input.Items) == 0 {
		return "items=0"
	}
	out := make([]string, 0, len(input.Items))
	for _, item := range input.Items {
		itemType := strings.TrimSpace(item.Type)
		if itemType == "" {
			itemType = "<empty_type>"
		}
		token := itemType
		if strings.TrimSpace(item.CallID) != "" {
			token += fmt.Sprintf("(call_id=%s)", strings.TrimSpace(item.CallID))
		}
		if itemType == "function_call_output" {
			token += fmt.Sprintf("(output_len=%d)", len(strings.TrimSpace(item.Output)))
		}
		out = append(out, token)
	}
	return fmt.Sprintf("items=%d[%s]", len(input.Items), strings.Join(out, ", "))
}

func summarizeToolCalls(calls []core.ToolCall) string {
	if len(calls) == 0 {
		return "<none>"
	}
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		out = append(out, fmt.Sprintf(
			"%s(call_id=%s,id=%s,response_id=%s,args_len=%d)",
			strings.TrimSpace(call.Name),
			strings.TrimSpace(call.CallID),
			strings.TrimSpace(call.ID),
			strings.TrimSpace(call.ResponseID),
			len(strings.TrimSpace(call.Arguments)),
		))
	}
	return strings.Join(out, ", ")
}
