package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
	}
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

	storeEnabled := resolveResponsesStoreFromContext(ctx)
	contextResult, buildErr := r.contextBuilder.Build(ContextBuildRequest{
		UserPrompt:   userPrompt,
		StoreEnabled: storeEnabled,
	})
	if buildErr != nil {
		return "", fmt.Errorf("build initial context failed: %w", buildErr)
	}
	req := contextResult.Request
	historyInputItems := cloneResponseInputItems(contextResult.HistoryInputItems)
	lastResponseTrace := ""
	initialPromptCurrentCommand := contextResult.InitialPromptCurrentCommand
	hasInitialPromptCurrentCommand := contextResult.HasInitialPromptCurrentCommand
	previousRoundMode := ""
	forceRoundModeHintNextIteration := false
	stalePromptCurrentCommandDetected := false
	stalePromptCurrentCommandReasons := map[string]struct{}{}
	promptCurrentCommandCleared := false

	for i := 0; i < r.options.MaxIterations; i++ {
		iteration := i + 1
		allowedTools, allowlistConfigured := allowedToolNameSetFromContext(ctx)
		if r.tools != nil {
			req.Tools = r.resolveToolSpecs(allowedTools, allowlistConfigured)
		}

		currentRoundMode := resolveRoundModeFromAllowedTools(allowedTools, allowlistConfigured)
		modeChanged := previousRoundMode != "" && currentRoundMode != "" && currentRoundMode != previousRoundMode
		if !promptCurrentCommandCleared && hasInitialPromptCurrentCommand && (modeChanged || stalePromptCurrentCommandDetected) {
			clearReasons := make([]string, 0, 4)
			if modeChanged {
				clearReasons = append(clearReasons, "mode_changed")
			}
			clearReasons = append(clearReasons, sortedReasonKeys(stalePromptCurrentCommandReasons)...)
			if updatedInput, changed := clearCurrentCommandFromResponseInput(req.Input); changed {
				req.Input = updatedInput
				historyInputItems = cloneResponseInputItems(updatedInput.Items)
				promptCurrentCommandCleared = true
				forceRoundModeHintNextIteration = true
				stalePromptCurrentCommandDetected = false
				stalePromptCurrentCommandReasons = map[string]struct{}{}
				emitToolEvent(onToolEvent, ContextRewriteEvent{
					Iteration:           iteration,
					Timestamp:           time.Now(),
					ClearReasons:        clearReasons,
					PreviousRoundMode:   previousRoundMode,
					CurrentRoundMode:    currentRoundMode,
					InitialCurrentCmd:   initialPromptCurrentCommand,
					HistoryItemsUpdated: true,
				})
			}
		}

		callReq := req
		injectRoundModeHint := forceRoundModeHintNextIteration || modeChanged
		callReq.Input = withRoundModeHintInputWhen(req.Input, allowedTools, allowlistConfigured, injectRoundModeHint)
		forceRoundModeHintNextIteration = false
		if err := core.ValidateResponseInputInvariants(callReq.Input); err != nil {
			base := fmt.Sprintf("responses input invariant failed iteration=%d %s", iteration, summarizeCreateResponseRequest(callReq))
			return "", fmt.Errorf("%s: %w", base, err)
		}
		reqSummary := summarizeCreateResponseRequest(callReq)
		emitToolEvent(onToolEvent, ModelRequestEvent{
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
		if onTextDelta != nil {
			if streamClient, ok := r.client.(ResponsesStreamAPI); ok {
				res, err = streamClient.CreateResponseStream(callCtx, callReq, onTextDelta)
			} else {
				res, err = r.client.CreateResponse(callCtx, callReq)
				if err == nil && strings.TrimSpace(res.FinalText) != "" {
					onTextDelta(res.FinalText)
				}
			}
		} else {
			res, err = r.client.CreateResponse(callCtx, callReq)
		}
		if err != nil {
			base := fmt.Sprintf("responses request failed iteration=%d %s", iteration, reqSummary)
			if strings.TrimSpace(lastResponseTrace) != "" {
				base += " prev_response_trace=" + lastResponseTrace
			}
			return "", fmt.Errorf("%s: %w", base, err)
		}

		currentTrace := summarizeEventTrace(res.EventTrace)
		emitToolEvent(onToolEvent, ModelResponseEvent{
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

			emitToolEvent(onToolEvent, ToolInputEvent{
				Iteration:    iteration,
				Timestamp:    time.Now(),
				CallID:       callID,
				ResponseID:   strings.TrimSpace(res.ID),
				ToolName:     strings.TrimSpace(call.Name),
				Input:        normalizeJSONText(call.Arguments),
				InputRawLen:  len(strings.TrimSpace(call.Arguments)),
				InputPreview: clipForLog(strings.TrimSpace(call.Arguments), 800),
			})

			out, outputErrText, outputIsError := r.executeToolCall(ctx, allowedTools, allowlistConfigured, call)
			outputState := "output-available"
			if outputIsError {
				outputState = "output-error"
			}
			emitToolEvent(onToolEvent, ToolOutputEvent{
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
			if outputCommand, hashChanged, ok := detectPostTerminalStateFromToolOutput(out); ok {
				if hashChanged {
					stalePromptCurrentCommandDetected = true
					stalePromptCurrentCommandReasons["hash_changed"] = struct{}{}
				}
				if hasInitialPromptCurrentCommand && strings.TrimSpace(outputCommand) != "" &&
					!strings.EqualFold(strings.TrimSpace(outputCommand), strings.TrimSpace(initialPromptCurrentCommand)) {
					stalePromptCurrentCommandDetected = true
					stalePromptCurrentCommandReasons["current_command_changed"] = struct{}{}
				}
				if stalePromptCurrentCommandDetected {
					forceRoundModeHintNextIteration = true
				}
			}

			replayCall := buildReplayFunctionCallInputItem(call)
			outputItem := core.ResponseInputItem{Type: "function_call_output", CallID: callID, Output: out}
			outputs = append(outputs, outputItem)
			replayItems = append(replayItems, replayCall, outputItem)
		}

		emitToolEvent(onToolEvent, RoundtripPreparedEvent{
			Iteration:          iteration,
			Timestamp:          time.Now(),
			PreviousResponseID: strings.TrimSpace(res.ID),
			RoundtripMode:      roundtripModeName(true),
			ItemsCount:         len(outputs),
			ItemsSummary:       summarizeResponseInput(core.NewResponseInputItems(outputs)),
		})

		historyInputItems = append(historyInputItems, replayItems...)
		req = core.CreateResponseRequest{
			Store: boolPtr(storeEnabled),
			Input: core.NewResponseInputItems(cloneResponseInputItems(historyInputItems)),
		}
		previousRoundMode = currentRoundMode
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

func emitToolEvent(onToolEvent func(LoopEvent), event LoopEvent) {
	if onToolEvent == nil {
		return
	}
	onToolEvent(event)
}

func withRoundModeHintInputWhen(
	input core.ResponseInput,
	allowedTools map[string]struct{},
	allowlistConfigured bool,
	enable bool,
) core.ResponseInput {
	if !enable || len(input.Items) == 0 {
		return input
	}
	hint := buildRoundModeHintInputItem(allowedTools, allowlistConfigured)
	if hint.Type == "" {
		return input
	}
	items := cloneResponseInputItems(input.Items)
	out := make([]core.ResponseInputItem, 0, len(items)+1)
	if len(items) > 0 {
		first := items[0]
		if strings.TrimSpace(first.Type) == "message" && strings.TrimSpace(first.Role) == "system" {
			out = append(out, first, hint)
			out = append(out, items[1:]...)
			return core.NewResponseInputItems(out)
		}
	}
	out = append(out, hint)
	out = append(out, items...)
	return core.NewResponseInputItems(out)
}

func resolveRoundModeFromAllowedTools(allowedTools map[string]struct{}, allowlistConfigured bool) string {
	if !allowlistConfigured {
		return "unconstrained"
	}
	_, hasExecCommand := allowedTools["exec_command"]
	_, hasTaskInputPrompt := allowedTools["task.input_prompt"]
	switch {
	case hasTaskInputPrompt && !hasExecCommand:
		return "ai_agent"
	case hasExecCommand && !hasTaskInputPrompt:
		return "shell"
	case hasExecCommand && hasTaskInputPrompt:
		return "mixed"
	default:
		return "default"
	}
}

func clearCurrentCommandFromResponseInput(input core.ResponseInput) (core.ResponseInput, bool) {
	if len(input.Items) == 0 {
		return input, false
	}
	updated, changed := clearCurrentCommandInInputItems(input.Items)
	if !changed {
		return input, false
	}
	return core.NewResponseInputItems(updated), true
}

func clearCurrentCommandInInputItems(items []core.ResponseInputItem) ([]core.ResponseInputItem, bool) {
	if len(items) == 0 {
		return items, false
	}
	out := cloneResponseInputItems(items)
	changed := false
	for i, item := range items {
		if strings.TrimSpace(item.Type) != "message" || strings.TrimSpace(item.Role) != "user" {
			continue
		}
		nextParts := make([]core.ResponseInputContentPart, len(item.Content))
		copy(nextParts, item.Content)
		partChanged := false
		for idx, part := range item.Content {
			if strings.TrimSpace(part.Type) != "input_text" {
				continue
			}
			nextText, textChanged := clearCurrentCommandInPromptText(part.Text)
			if !textChanged {
				continue
			}
			nextParts[idx].Text = nextText
			partChanged = true
		}
		if !partChanged {
			continue
		}
		updatedItem := item
		updatedItem.Content = nextParts
		out[i] = updatedItem
		changed = true
	}
	if !changed {
		return items, false
	}
	return out, true
}

var promptCurrentCommandPattern = regexp.MustCompile(`"current_command"\s*:\s*"((?:\\.|[^"\\])*)"`)

func extractPromptCurrentCommand(prompt string) (string, bool) {
	start, end, ok := extractPromptJSONObjectRange(prompt, "terminal_screen_state_json:")
	if !ok {
		return "", false
	}
	segment := prompt[start:end]
	matches := promptCurrentCommandPattern.FindStringSubmatch(segment)
	if len(matches) < 2 {
		return "", false
	}
	decoded, err := strconv.Unquote("\"" + matches[1] + "\"")
	if err != nil {
		return "", false
	}
	decoded = strings.TrimSpace(decoded)
	if decoded == "" {
		return "", false
	}
	return decoded, true
}

func clearCurrentCommandInPromptText(prompt string) (string, bool) {
	start, end, ok := extractPromptJSONObjectRange(prompt, "terminal_screen_state_json:")
	if !ok {
		return prompt, false
	}
	segment := prompt[start:end]
	loc := promptCurrentCommandPattern.FindStringIndex(segment)
	if loc == nil {
		return prompt, false
	}
	replaced := promptCurrentCommandPattern.ReplaceAllString(segment, `"current_command":""`)
	if replaced == segment {
		return prompt, false
	}
	return prompt[:start] + replaced + prompt[end:], true
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

type postTerminalScreenStateEnvelope struct {
	Data struct {
		PostTerminalScreenState postTerminalScreenState `json:"post_terminal_screen_state"`
	} `json:"data"`
	PostTerminalScreenState postTerminalScreenState `json:"post_terminal_screen_state"`
}

type postTerminalScreenState struct {
	CurrentCommand string `json:"current_command"`
	HashChanged    bool   `json:"hash_changed"`
}

func detectPostTerminalStateFromToolOutput(output string) (string, bool, bool) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return "", false, false
	}
	var decoded postTerminalScreenStateEnvelope
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return "", false, false
	}
	screen := decoded.Data.PostTerminalScreenState
	if strings.TrimSpace(screen.CurrentCommand) == "" && !screen.HashChanged {
		screen = decoded.PostTerminalScreenState
	}
	if strings.TrimSpace(screen.CurrentCommand) == "" && !screen.HashChanged {
		return "", false, false
	}
	return strings.TrimSpace(screen.CurrentCommand), screen.HashChanged, true
}

func sortedReasonKeys(reasons map[string]struct{}) []string {
	if len(reasons) == 0 {
		return []string{}
	}
	keys := make([]string, 0, len(reasons))
	for key := range reasons {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func buildRoundModeHintInputItem(allowedTools map[string]struct{}, allowlistConfigured bool) core.ResponseInputItem {
	mode := "default"
	if allowlistConfigured {
		if _, ok := allowedTools["task.input_prompt"]; ok {
			mode = "ai_agent"
		} else if _, ok := allowedTools["exec_command"]; ok {
			mode = "shell"
		}
	}
	names := make([]string, 0, len(allowedTools))
	for name := range allowedTools {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	allowlistState := "resolver_disabled"
	if allowlistConfigured {
		allowlistState = "resolver_enabled"
	}
	text := strings.TrimSpace(strings.Join([]string{
		"ROUND_MODE_HINT",
		fmt.Sprintf("allowlist=%s", allowlistState),
		fmt.Sprintf("mode=%s", mode),
		fmt.Sprintf("allowed_tools=%s", strings.Join(names, ",")),
		"Authoritative mode for this round is derived from allowed_tools; ignore stale terminal snapshots from earlier messages.",
		"Never call tools that are not listed in allowed_tools for this round.",
	}, "\n"))
	return core.ResponseInputItem{
		Type: "message",
		Role: "user",
		Content: []core.ResponseInputContentPart{{
			Type: "input_text",
			Text: text,
		}},
	}
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

func allowedToolNameSetFromContext(ctx context.Context) (map[string]struct{}, bool) {
	names, ok := AllowedToolNamesFromContext(ctx)
	if !ok {
		return nil, false
	}
	out := map[string]struct{}{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	return out, true
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

func resolveResponsesStoreFromContext(ctx context.Context) bool {
	storeEnabled, _ := ResponsesStoreFromContext(ctx)
	return storeEnabled
}

func boolPtr(v bool) *bool {
	return &v
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
