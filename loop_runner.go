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
	transitionsMu  sync.RWMutex
	transitions    []TransitionRecord
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
	req, err := BuildContextRequestFromPrompt(userPrompt)
	if err != nil {
		return "", err
	}
	return r.runWithContextRequest(ctx, req, nil, nil)
}

func (r *LoopRunner) RunStream(ctx context.Context, userPrompt string, onTextDelta func(string)) (string, error) {
	req, err := BuildContextRequestFromPrompt(userPrompt)
	if err != nil {
		return "", err
	}
	return r.runWithContextRequest(ctx, req, onTextDelta, nil)
}

func (r *LoopRunner) RunStreamWithTools(
	ctx context.Context,
	userPrompt string,
	onTextDelta func(string),
	onToolEvent func(LoopEvent),
) (string, error) {
	req, err := BuildContextRequestFromPrompt(userPrompt)
	if err != nil {
		return "", err
	}
	return r.runWithContextRequest(ctx, req, onTextDelta, onToolEvent)
}

func (r *LoopRunner) RunWithContext(ctx context.Context, req ContextBuildRequest) (string, error) {
	return r.runWithContextRequest(ctx, req, nil, nil)
}

func (r *LoopRunner) RunStreamWithContext(
	ctx context.Context,
	req ContextBuildRequest,
	onTextDelta func(string),
) (string, error) {
	return r.runWithContextRequest(ctx, req, onTextDelta, nil)
}

func (r *LoopRunner) RunStreamWithContextAndTools(
	ctx context.Context,
	req ContextBuildRequest,
	onTextDelta func(string),
	onToolEvent func(LoopEvent),
) (string, error) {
	return r.runWithContextRequest(ctx, req, onTextDelta, onToolEvent)
}

func (r *LoopRunner) LastTransitions() []TransitionRecord {
	if r == nil {
		return nil
	}
	r.transitionsMu.RLock()
	defer r.transitionsMu.RUnlock()
	out := make([]TransitionRecord, len(r.transitions))
	copy(out, r.transitions)
	return out
}

func (r *LoopRunner) runWithContextRequest(
	ctx context.Context,
	contextReq ContextBuildRequest,
	onTextDelta func(string),
	onToolEvent func(LoopEvent),
) (string, error) {
	if r == nil || r.client == nil {
		return "", errors.New("loop runner client is required")
	}
	if r.contextBuilder == nil {
		return "", errors.New("context builder is required")
	}

	transitionRecords := make([]TransitionRecord, 0, 16)
	state := RunnerStateIdle
	guard := NewRunnerTransitionGuard()
	transition := func(event RunnerEvent, to RunnerState, iteration int, snapshot RunnerSnapshot) {
		if err := guard.Validate(state, event, to); err != nil {
			panic(err)
		}
		record := TransitionRecord{
			From:      state,
			Event:     event,
			To:        to,
			Iteration: iteration,
			Timestamp: time.Now(),
			Snapshot:  snapshot,
		}
		state = to
		transitionRecords = append(transitionRecords, record)
		r.emitLoopEvent(onToolEvent, TransitionEvent{Record: record})
	}
	defer func() {
		r.setLastTransitions(transitionRecords)
	}()

	transition(RunnerEventRunStarted, RunnerStatePreparingContext, 0, RunnerSnapshot{})
	contextResult, buildErr := r.contextBuilder.Build(contextReq)
	if buildErr != nil {
		transition(RunnerEventRunFailed, RunnerStateFailed, 0, RunnerSnapshot{LastError: buildErr.Error()})
		return "", fmt.Errorf("build initial context failed: %w", buildErr)
	}
	transition(RunnerEventContextBuilt, RunnerStateCallingModel, 0, RunnerSnapshot{
		RequestSummary: summarizeCreateResponseRequest(contextResult.Request),
		RoundtripMode:  roundtripModeName(true),
	})
	req := contextResult.Request
	historyInputItems := cloneResponseInputItems(contextResult.HistoryInputItems)
	lastResponseTrace := ""

	for i := 0; i < r.options.MaxIterations; i++ {
		iteration := i + 1
		transition(RunnerEventModelRequest, RunnerStateCallingModel, iteration, RunnerSnapshot{
			RequestSummary: summarizeCreateResponseRequest(req),
			RoundtripMode:  roundtripModeName(true),
		})
		callReq := req
		if err := core.ValidateResponseInputInvariants(callReq.Input); err != nil {
			base := fmt.Sprintf("responses input invariant failed iteration=%d %s", iteration, summarizeCreateResponseRequest(callReq))
			transition(RunnerEventRunFailed, RunnerStateFailed, iteration, RunnerSnapshot{
				RequestSummary: summarizeCreateResponseRequest(callReq),
				LastError:      err.Error(),
			})
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
			transition(RunnerEventRunFailed, RunnerStateFailed, iteration, RunnerSnapshot{
				RequestSummary: reqSummary,
				LastError:      err.Error(),
			})
			return "", fmt.Errorf("%s: %w", base, err)
		}
		if res == nil {
			transition(RunnerEventRunFailed, RunnerStateFailed, iteration, RunnerSnapshot{
				RequestSummary: reqSummary,
				LastError:      "nil model response",
			})
			return "", fmt.Errorf("responses request returned nil response iteration=%d %s", iteration, reqSummary)
		}

		currentTrace := summarizeEventTrace(res.EventTrace)
		transition(RunnerEventModelResponse, RunnerStateCallingModel, iteration, RunnerSnapshot{
			RequestSummary: reqSummary,
			ResponseID:     strings.TrimSpace(res.ID),
			ToolCalls:      len(res.ToolCalls),
			RoundtripMode:  roundtripModeName(true),
		})
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
			transition(RunnerEventRunCompleted, RunnerStateCompleted, iteration, RunnerSnapshot{
				RequestSummary: reqSummary,
				ResponseID:     strings.TrimSpace(res.ID),
				ToolCalls:      len(res.ToolCalls),
				RoundtripMode:  roundtripModeName(true),
			})
			return res.FinalText, nil
		}
		if len(res.ToolCalls) == 0 {
			transition(RunnerEventRunFailed, RunnerStateFailed, iteration, RunnerSnapshot{
				RequestSummary: reqSummary,
				ResponseID:     strings.TrimSpace(res.ID),
				LastError:      "model response has no final text and no tool calls",
			})
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
				transition(RunnerEventRunFailed, RunnerStateFailed, iteration, RunnerSnapshot{
					RequestSummary: reqSummary,
					ResponseID:     strings.TrimSpace(res.ID),
					LastError:      "tool call missing call_id",
				})
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
			transition(RunnerEventToolCallBegin, RunnerStateExecutingTools, iteration, RunnerSnapshot{
				RequestSummary: reqSummary,
				ResponseID:     strings.TrimSpace(res.ID),
				ToolCalls:      len(res.ToolCalls),
				RoundtripMode:  roundtripModeName(true),
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
				transition(RunnerEventRunFailed, RunnerStateFailed, iteration, RunnerSnapshot{
					RequestSummary: reqSummary,
					ResponseID:     strings.TrimSpace(res.ID),
					LastError:      hookErr.Error(),
				})
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
			transition(RunnerEventToolCallEnd, RunnerStateExecutingTools, iteration, RunnerSnapshot{
				RequestSummary: reqSummary,
				ResponseID:     strings.TrimSpace(res.ID),
				ToolCalls:      len(res.ToolCalls),
				RoundtripMode:  roundtripModeName(true),
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
		transition(RunnerEventRoundtripPrepared, RunnerStatePreparingRoundtrip, iteration, RunnerSnapshot{
			RequestSummary: reqSummary,
			ResponseID:     strings.TrimSpace(res.ID),
			ToolCalls:      len(res.ToolCalls),
			RoundtripMode:  roundtripModeName(true),
		})
		roundHookCtx := &HookContext{
			Ctx:       ctx,
			Iteration: iteration,
			Response:  res,
		}
		previousInputSummary := summarizeResponseInput(req.Input)
		err = r.runHookChain(HookPointRoundtrip, roundHookCtx, func() error {
			historyInputItems = append(historyInputItems, replayItems...)
			req = core.CreateResponseRequest{
				Input: core.NewResponseInputItems(cloneResponseInputItems(historyInputItems)),
			}
			roundHookCtx.Request = &req
			return nil
		})
		if err != nil {
			transition(RunnerEventRunFailed, RunnerStateFailed, iteration, RunnerSnapshot{
				RequestSummary: reqSummary,
				ResponseID:     strings.TrimSpace(res.ID),
				LastError:      err.Error(),
			})
			return "", fmt.Errorf("roundtrip hook failed iteration=%d: %w", iteration, err)
		}
		if roundHookCtx.Request != nil {
			req = *roundHookCtx.Request
		}
		currentInputSummary := summarizeResponseInput(req.Input)
		historyItemsUpdated := previousInputSummary != currentInputSummary
		if historyItemsUpdated {
			r.emitLoopEvent(onToolEvent, ContextRewriteEvent{
				Iteration:           iteration,
				Timestamp:           time.Now(),
				ClearReasons:        []string{"roundtrip_replay_items_appended"},
				PreviousRoundMode:   roundtripModeName(true),
				CurrentRoundMode:    roundtripModeName(true),
				InitialCurrentCmd:   "",
				HistoryItemsUpdated: true,
			})
			transition(RunnerEventContextRewritten, RunnerStateCallingModel, iteration, RunnerSnapshot{
				RequestSummary: summarizeCreateResponseRequest(req),
				ResponseID:     strings.TrimSpace(res.ID),
				ToolCalls:      len(res.ToolCalls),
				RoundtripMode:  roundtripModeName(true),
			})
		} else {
			transition(RunnerEventContextRewritten, RunnerStateCallingModel, iteration, RunnerSnapshot{
				RequestSummary: summarizeCreateResponseRequest(req),
				ResponseID:     strings.TrimSpace(res.ID),
				ToolCalls:      len(res.ToolCalls),
				RoundtripMode:  roundtripModeName(true),
			})
		}
	}
	transition(RunnerEventRunFailed, RunnerStateFailed, r.options.MaxIterations, RunnerSnapshot{
		LastError: "responses loop exceeded max iterations",
	})
	return "", fmt.Errorf("responses loop exceeded max iterations: %d", r.options.MaxIterations)
}

func (r *LoopRunner) executeToolCall(
	ctx context.Context,
	allowedTools map[string]struct{},
	allowlistConfigured bool,
	call core.ToolCall,
) (string, string, bool) {
	pipeline := NewToolPipeline(r.tools)
	toolOut, toolErr := pipeline.Execute(ctx, ToolPipelineInput{
		AllowedTools:        allowedTools,
		AllowlistConfigured: allowlistConfigured,
		ToolCall:            call,
	})
	if toolErr != nil {
		return mustMarshalToolError(toolErr), toolErr.ErrorString, true
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

func (r *LoopRunner) setLastTransitions(records []TransitionRecord) {
	if r == nil {
		return
	}
	r.transitionsMu.Lock()
	defer r.transitionsMu.Unlock()
	r.transitions = make([]TransitionRecord, len(records))
	copy(r.transitions, records)
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
	return buildRoleMessageInputItem("user", text)
}

func buildSystemMessageInputItem(text string) core.ResponseInputItem {
	return buildRoleMessageInputItem("system", text)
}

func buildRoleMessageInputItem(role string, text string) core.ResponseInputItem {
	role = strings.TrimSpace(role)
	if role == "" {
		role = "user"
	}
	return core.ResponseInputItem{
		Type: "message",
		Role: role,
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
