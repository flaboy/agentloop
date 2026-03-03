package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	core "github.com/flaboy/agentloop/core"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/responses"
)

type OpenAIConfig struct {
	BaseURL          string
	Model            string
	APIKey           string
	ReasoningEffort  string
	UseResponsesAPI  bool
	EnableState      bool
	AuthProvider     AuthProvider
	EndpointProvider EndpointProvider
	RequestMutator   RequestMutator
}

type ResponsesClient struct {
	cfg     OpenAIConfig
	service responses.ResponseService
}

func NewResponsesClient(cfg OpenAIConfig, httpClient *http.Client) *ResponsesClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	httpClient = wrapHTTPClientWithMutator(httpClient, cfg.RequestMutator)
	opts := []option.RequestOption{option.WithHTTPClient(httpClient)}
	if base := strings.TrimSpace(cfg.BaseURL); base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	if key := strings.TrimSpace(cfg.APIKey); key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}
	return &ResponsesClient{
		cfg:     cfg,
		service: responses.NewResponseService(opts...),
	}
}

func (c *ResponsesClient) CreateResponse(ctx context.Context, req core.CreateResponseRequest) (*core.CreateResponseResult, error) {
	if c == nil {
		return nil, errors.New("responses client is nil")
	}
	if !c.cfg.UseResponsesAPI {
		return nil, errors.New("responses api is disabled by config")
	}
	req.Stream = false
	params, err := c.toSDKRequest(req)
	if err != nil {
		return nil, err
	}
	var rawResp *http.Response
	var rawBody []byte
	requestOpts := []option.RequestOption{
		option.WithResponseInto(&rawResp),
		option.WithResponseBodyInto(&rawBody),
	}
	if c.cfg.EnableState {
		requestOpts = append(requestOpts, option.WithJSONSet("state", true))
	}
	requestOpts, err = c.requestOptions(ctx, requestOpts)
	if err != nil {
		return nil, err
	}
	_, err = c.service.New(ctx, params, requestOpts...)
	if err != nil {
		return nil, c.wrapRequestError(err, req, rawResp)
	}
	if len(rawBody) == 0 {
		return nil, fmt.Errorf("responses api returned empty response request=%s", summarizeCreateResponseRequest(req))
	}
	return parseResponseResult(rawBody)
}

func (c *ResponsesClient) CreateResponseStream(ctx context.Context, req core.CreateResponseRequest, onTextDelta func(string)) (*core.CreateResponseResult, error) {
	if c == nil {
		return nil, errors.New("responses client is nil")
	}
	if !c.cfg.UseResponsesAPI {
		return nil, errors.New("responses api is disabled by config")
	}
	req.Stream = true
	params, err := c.toSDKRequest(req)
	if err != nil {
		return nil, err
	}
	var rawResp *http.Response
	requestOpts := []option.RequestOption{option.WithResponseInto(&rawResp)}
	if c.cfg.EnableState {
		requestOpts = append(requestOpts, option.WithJSONSet("state", true))
	}
	requestOpts, err = c.requestOptions(ctx, requestOpts)
	if err != nil {
		return nil, err
	}
	stream := c.service.NewStreaming(ctx, params, requestOpts...)
	if stream == nil {
		return nil, fmt.Errorf("responses stream unavailable request=%s", summarizeCreateResponseRequest(req))
	}
	defer func() {
		_ = stream.Close()
	}()

	out := &core.CreateResponseResult{}
	eventTrace := make([]string, 0, 32)
	sawTextDelta := false
	toolCallOrder := make([]string, 0, 8)
	toolCallsByKey := map[string]*core.ToolCall{}
	itemToKey := map[string]string{}
	argumentByKey := map[string]string{}

	resolveToolCallKey := func(candidates ...string) string {
		normalized := make([]string, 0, len(candidates))
		for _, raw := range candidates {
			key := strings.TrimSpace(raw)
			if key == "" {
				continue
			}
			normalized = append(normalized, key)
		}
		for _, key := range normalized {
			if mapped := strings.TrimSpace(itemToKey[key]); mapped != "" {
				return mapped
			}
		}
		for _, key := range normalized {
			if _, ok := toolCallsByKey[key]; ok {
				return key
			}
		}
		if len(normalized) > 0 {
			return normalized[0]
		}
		return ""
	}
	ensureToolCall := func(key string) *core.ToolCall {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil
		}
		if existing, ok := toolCallsByKey[key]; ok {
			return existing
		}
		tc := &core.ToolCall{}
		toolCallsByKey[key] = tc
		toolCallOrder = append(toolCallOrder, key)
		return tc
	}
	rememberItemKey := func(itemID, key string) {
		itemID = strings.TrimSpace(itemID)
		key = strings.TrimSpace(key)
		if itemID == "" || key == "" {
			return
		}
		itemToKey[itemID] = key
	}
	buildToolCalls := func() []core.ToolCall {
		outCalls := make([]core.ToolCall, 0, len(toolCallOrder))
		for _, key := range toolCallOrder {
			tc := toolCallsByKey[key]
			if tc == nil {
				continue
			}
			call := *tc
			if strings.TrimSpace(call.ResponseID) == "" {
				call.ResponseID = strings.TrimSpace(out.ID)
			}
			outCalls = append(outCalls, call)
		}
		return outCalls
	}
	setArguments := func(arguments string, candidates ...string) {
		if strings.TrimSpace(arguments) == "" {
			return
		}
		key := resolveToolCallKey(candidates...)
		if key == "" {
			return
		}
		tc := ensureToolCall(key)
		if tc == nil {
			return
		}
		argumentByKey[key] = arguments
		tc.Arguments = strings.TrimSpace(argumentByKey[key])
		for _, candidate := range candidates {
			rememberItemKey(candidate, key)
		}
	}
	appendArgumentsDelta := func(delta string, candidates ...string) {
		if strings.TrimSpace(delta) == "" {
			return
		}
		key := resolveToolCallKey(candidates...)
		if key == "" {
			return
		}
		tc := ensureToolCall(key)
		if tc == nil {
			return
		}
		argumentByKey[key] += delta
		if merged := strings.TrimSpace(argumentByKey[key]); merged != "" {
			tc.Arguments = merged
		}
		for _, candidate := range candidates {
			rememberItemKey(candidate, key)
		}
	}
	mergeToolCall := func(call core.ToolCall, eventItemID, eventResponseID string) {
		key := resolveToolCallKey(call.CallID, call.ID, eventItemID)
		if key == "" {
			return
		}
		tc := ensureToolCall(key)
		if tc == nil {
			return
		}
		inID := strings.TrimSpace(call.ID)
		inCallID := strings.TrimSpace(call.CallID)
		if inID != "" && (strings.TrimSpace(tc.ID) == "" || strings.TrimSpace(tc.ID) == strings.TrimSpace(tc.CallID)) {
			tc.ID = inID
		}
		if inCallID != "" && (strings.TrimSpace(tc.CallID) == "" || strings.TrimSpace(tc.CallID) == strings.TrimSpace(tc.ID)) {
			tc.CallID = inCallID
		}
		if strings.TrimSpace(tc.Name) == "" {
			tc.Name = strings.TrimSpace(call.Name)
		}
		if strings.TrimSpace(tc.ResponseID) == "" {
			tc.ResponseID = strings.TrimSpace(call.ResponseID)
			if strings.TrimSpace(tc.ResponseID) == "" {
				tc.ResponseID = strings.TrimSpace(eventResponseID)
			}
		}
		deltaArgs := strings.TrimSpace(argumentByKey[key])
		itemArgs := strings.TrimSpace(call.Arguments)
		if deltaArgs != "" {
			tc.Arguments = deltaArgs
		} else if itemArgs != "" {
			tc.Arguments = call.Arguments
		}
		rememberItemKey(call.ID, key)
		rememberItemKey(call.CallID, key)
		rememberItemKey(eventItemID, key)
	}

	processEvent := func(data string) error {
		data = strings.TrimSpace(data)
		if data == "" {
			return nil
		}
		var event struct {
			Type       string          `json:"type"`
			Delta      string          `json:"delta"`
			ResponseID string          `json:"response_id"`
			ItemID     string          `json:"item_id"`
			Name       string          `json:"name"`
			Arguments  string          `json:"arguments"`
			Sequence   int             `json:"sequence_number"`
			Item       responseItem    `json:"item"`
			Response   responsePayload `json:"response"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return fmt.Errorf("invalid responses stream event: %w data=%q", err, clipForLog(data, 600))
		}
		eventResponseID := strings.TrimSpace(event.Response.ID)
		if eventResponseID == "" {
			eventResponseID = strings.TrimSpace(event.ResponseID)
		}
		if eventResponseID == "" {
			eventResponseID = strings.TrimSpace(event.Item.ResponseID)
		}
		if out.ID == "" && eventResponseID != "" {
			out.ID = eventResponseID
		}
		eventTrace = appendEventTrace(eventTrace, summarizeStreamEventForTrace(event.Type, eventResponseID, event.Item, event.ItemID, event.Name, event.Arguments, event.Sequence))
		switch strings.TrimSpace(event.Type) {
		case "response.created":
		case "response.output_text.delta":
			if event.Delta == "" {
				return nil
			}
			sawTextDelta = true
			out.FinalText += event.Delta
			if onTextDelta != nil {
				onTextDelta(event.Delta)
			}
		case "response.output_item.added", "response.output_item.done":
			if call, ok := toToolCall(event.Item); ok {
				if strings.TrimSpace(call.ResponseID) == "" {
					call.ResponseID = eventResponseID
				}
				mergeToolCall(call, event.ItemID, eventResponseID)
				out.ToolCalls = buildToolCalls()
				return nil
			}
			if !sawTextDelta {
				appendMessageText(out, event.Item.Content)
			}
		case "response.function_call_arguments.delta":
			appendArgumentsDelta(event.Delta, event.ItemID, event.Item.ID, event.Item.CallID)
			out.ToolCalls = buildToolCalls()
		case "response.function_call_arguments.done":
			setArguments(event.Arguments, event.ItemID, event.Item.ID, event.Item.CallID)
			out.ToolCalls = buildToolCalls()
		case "response.failed":
			respID := strings.TrimSpace(eventResponseID)
			if respID == "" {
				respID = strings.TrimSpace(out.ID)
			}
			code := ""
			message := ""
			if event.Response.Error != nil {
				code = strings.TrimSpace(event.Response.Error.Code)
				message = strings.TrimSpace(event.Response.Error.Message)
			}
			return fmt.Errorf("responses api failed response_id=%q code=%q message=%q", respID, code, message)
		case "response.completed":
			for _, item := range event.Response.Output {
				if call, ok := toToolCall(item); ok {
					if strings.TrimSpace(call.ResponseID) == "" {
						call.ResponseID = eventResponseID
					}
					mergeToolCall(call, item.ID, eventResponseID)
					continue
				}
				if !sawTextDelta {
					appendMessageText(out, item.Content)
				}
			}
			out.ToolCalls = buildToolCalls()
		}
		return nil
	}

	for stream.Next() {
		event := stream.Current()
		if err := processEvent(event.RawJSON()); err != nil {
			return nil, err
		}
	}
	if err := stream.Err(); err != nil {
		return nil, c.wrapRequestError(err, req, rawResp)
	}
	out.ToolCalls = buildToolCalls()
	out.EventTrace = append([]string(nil), eventTrace...)
	return out, nil
}

func (c *ResponsesClient) toSDKRequest(req core.CreateResponseRequest) (responses.ResponseNewParams, error) {
	if err := core.ValidateResponseInputInvariants(req.Input); err != nil {
		return responses.ResponseNewParams{}, fmt.Errorf("invalid responses input invariants: %w", err)
	}
	var out responses.ResponseNewParams
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(c.cfg.Model)
	}
	if model != "" {
		out.Model = model
	}
	if effort := strings.ToLower(strings.TrimSpace(c.cfg.ReasoningEffort)); effort != "" {
		switch effort {
		case string(responses.ReasoningEffortLow):
			out.Reasoning = responses.ReasoningParam{Effort: responses.ReasoningEffortLow}
		case string(responses.ReasoningEffortMedium):
			out.Reasoning = responses.ReasoningParam{Effort: responses.ReasoningEffortMedium}
		case string(responses.ReasoningEffortHigh):
			out.Reasoning = responses.ReasoningParam{Effort: responses.ReasoningEffortHigh}
		default:
			return responses.ResponseNewParams{}, fmt.Errorf("invalid reasoning effort: %q", effort)
		}
	}
	if req.Store != nil {
		out.Store = param.NewOpt(*req.Store)
	}
	if prev := strings.TrimSpace(req.PreviousResponseID); prev != "" {
		out.PreviousResponseID = param.NewOpt(prev)
	}
	input, err := toSDKInput(req.Input)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	out.Input = input
	if len(req.Tools) > 0 {
		tools, err := toSDKTools(req.Tools)
		if err != nil {
			return responses.ResponseNewParams{}, err
		}
		out.Tools = tools
	}
	return out, nil
}

func (c *ResponsesClient) requestOptions(ctx context.Context, opts []option.RequestOption) ([]option.RequestOption, error) {
	out := make([]option.RequestOption, 0, len(opts)+2)
	out = append(out, opts...)
	if c != nil && c.cfg.EndpointProvider != nil {
		baseURL, err := c.cfg.EndpointProvider.ResolveEndpoint(ctx)
		if err != nil {
			return nil, err
		}
		baseURL = strings.TrimSpace(baseURL)
		if baseURL != "" {
			out = append(out, option.WithBaseURL(baseURL))
		}
	}
	if c != nil && c.cfg.AuthProvider != nil {
		cred, err := c.cfg.AuthProvider.ResolveAuth(ctx)
		if err != nil {
			return nil, err
		}
		headerName := strings.TrimSpace(cred.HeaderName)
		headerValue := strings.TrimSpace(cred.HeaderValue)
		if headerName == "" {
			headerName = "Authorization"
		}
		if headerValue != "" {
			out = append(out, option.WithHeader(headerName, headerValue))
		}
	}
	return out, nil
}

type requestMutatingRoundTripper struct {
	base    http.RoundTripper
	mutator RequestMutator
}

func (rt requestMutatingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return rt.base.RoundTrip(req)
	}
	if rt.mutator != nil {
		if err := rt.mutator.MutateRequest(req.Context(), req); err != nil {
			return nil, err
		}
	}
	return rt.base.RoundTrip(req)
}

func wrapHTTPClientWithMutator(base *http.Client, mutator RequestMutator) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	if mutator == nil {
		return base
	}
	clone := *base
	transport := clone.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	clone.Transport = requestMutatingRoundTripper{
		base:    transport,
		mutator: mutator,
	}
	return &clone
}

func toSDKInput(input core.ResponseInput) (responses.ResponseNewParamsInputUnion, error) {
	var out responses.ResponseNewParamsInputUnion
	if strings.TrimSpace(input.Text) == "" && len(input.Items) == 0 {
		return out, nil
	}
	if strings.TrimSpace(input.Text) != "" {
		out.OfString = param.NewOpt(input.Text)
		return out, nil
	}
	items := make(responses.ResponseInputParam, 0, len(input.Items))
	for i, rawItem := range input.Items {
		item, err := toSDKInputItem(rawItem)
		if err != nil {
			return responses.ResponseNewParamsInputUnion{}, fmt.Errorf("invalid response input item[%d]: %w", i, err)
		}
		items = append(items, item)
	}
	out.OfInputItemList = items
	return out, nil
}

func toSDKInputItem(rawItem core.ResponseInputItem) (responses.ResponseInputItemUnionParam, error) {
	raw, err := json.Marshal(rawItem)
	if err != nil {
		return responses.ResponseInputItemUnionParam{}, fmt.Errorf("marshal response input item failed: %w", err)
	}
	var out responses.ResponseInputItemUnionParam
	if err := json.Unmarshal(raw, &out); err != nil {
		return responses.ResponseInputItemUnionParam{}, fmt.Errorf("decode response input item failed: %w", err)
	}
	return out, nil
}

func toSDKTools(tools []core.ResponseToolSpec) ([]responses.ToolUnionParam, error) {
	out := make([]responses.ToolUnionParam, 0, len(tools))
	for i, spec := range tools {
		raw, err := json.Marshal(spec)
		if err != nil {
			return nil, fmt.Errorf("marshal response tool[%d] failed: %w", i, err)
		}
		var tool responses.ToolUnionParam
		if err := json.Unmarshal(raw, &tool); err != nil {
			return nil, fmt.Errorf("decode response tool[%d] failed: %w", i, err)
		}
		out = append(out, tool)
	}
	return out, nil
}

func parseResponseResult(raw []byte) (*core.CreateResponseResult, error) {
	var decoded responsePayload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	if strings.EqualFold(strings.TrimSpace(decoded.Status), "failed") {
		code := ""
		message := ""
		if decoded.Error != nil {
			code = strings.TrimSpace(decoded.Error.Code)
			message = strings.TrimSpace(decoded.Error.Message)
		}
		return nil, fmt.Errorf("responses api failed response_id=%q code=%q message=%q", strings.TrimSpace(decoded.ID), code, message)
	}
	out := &core.CreateResponseResult{ID: strings.TrimSpace(decoded.ID)}
	for _, item := range decoded.Output {
		if call, ok := toToolCall(item); ok {
			if strings.TrimSpace(call.ResponseID) == "" {
				call.ResponseID = out.ID
			}
			out.ToolCalls = append(out.ToolCalls, call)
			continue
		}
		appendMessageText(out, item.Content)
	}
	return out, nil
}

func (c *ResponsesClient) wrapRequestError(err error, req core.CreateResponseRequest, rawResp *http.Response) error {
	var apiErr *responses.Error
	if errors.As(err, &apiErr) {
		resp := rawResp
		if resp == nil {
			resp = apiErr.Response
		}
		body := strings.TrimSpace(apiErr.RawJSON())
		if body == "" {
			body = strings.TrimSpace(err.Error())
		}
		return fmt.Errorf(
			"responses api status %d request_id=%q headers=%s request=%s response=%s",
			apiErr.StatusCode,
			responseRequestID(resp),
			summarizeResponseHeaders(resp),
			summarizeCreateResponseRequest(req),
			body,
		)
	}
	return fmt.Errorf("responses request failed request=%s: %w", summarizeCreateResponseRequest(req), err)
}

type responseContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responseItem struct {
	Type       string                `json:"type"`
	ID         string                `json:"id"`
	ResponseID string                `json:"response_id"`
	CallID     string                `json:"call_id"`
	Name       string                `json:"name"`
	Arguments  string                `json:"arguments"`
	Content    []responseContentPart `json:"content"`
}

type responsePayload struct {
	ID     string           `json:"id"`
	Status string           `json:"status"`
	Error  *responseFailure `json:"error"`
	Output []responseItem   `json:"output"`
}

type responseFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func responseRequestID(resp *http.Response) string {
	if resp == nil || resp.Header == nil {
		return ""
	}
	for _, key := range []string{"x-request-id", "request-id", "openai-request-id", "x-openai-request-id"} {
		value := strings.TrimSpace(resp.Header.Get(key))
		if value != "" {
			return value
		}
	}
	return ""
}

func summarizeResponseHeaders(resp *http.Response) string {
	if resp == nil || resp.Header == nil {
		return "{}"
	}
	keys := []string{
		"x-request-id",
		"request-id",
		"openai-request-id",
		"x-openai-request-id",
		"openrouter-request-id",
		"openrouter-model",
		"via",
		"server",
		"cf-ray",
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := strings.TrimSpace(resp.Header.Get(k))
		if v == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%q", k, v))
	}
	if len(parts) == 0 {
		return "{}"
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func toToolCall(item responseItem) (core.ToolCall, bool) {
	if strings.TrimSpace(item.Type) != "function_call" {
		return core.ToolCall{}, false
	}
	return core.ToolCall{
		ID:         strings.TrimSpace(item.ID),
		CallID:     strings.TrimSpace(item.CallID),
		ResponseID: strings.TrimSpace(item.ResponseID),
		Name:       strings.TrimSpace(item.Name),
		Arguments:  strings.TrimSpace(item.Arguments),
	}, true
}

func appendMessageText(out *core.CreateResponseResult, parts []responseContentPart) {
	if out == nil {
		return
	}
	for _, content := range parts {
		if strings.TrimSpace(content.Type) != "output_text" || strings.TrimSpace(content.Text) == "" {
			continue
		}
		if out.FinalText == "" {
			out.FinalText = content.Text
		} else {
			out.FinalText += "\n" + content.Text
		}
	}
}

func appendEventTrace(trace []string, entry string) []string {
	if strings.TrimSpace(entry) == "" {
		return trace
	}
	if len(trace) >= 40 {
		trace = trace[1:]
	}
	return append(trace, entry)
}

func summarizeStreamEventForTrace(
	eventType string,
	responseID string,
	item responseItem,
	itemID string,
	name string,
	arguments string,
	sequence int,
) string {
	eventType = strings.TrimSpace(eventType)
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		responseID = strings.TrimSpace(item.ResponseID)
	}
	itemType := strings.TrimSpace(item.Type)
	if itemType == "" && strings.Contains(eventType, "function_call_arguments") {
		itemType = "function_call_arguments"
	}
	callID := strings.TrimSpace(item.CallID)
	if callID == "" {
		callID = strings.TrimSpace(item.ID)
	}
	resolvedItemID := strings.TrimSpace(item.ID)
	if resolvedItemID == "" {
		resolvedItemID = strings.TrimSpace(itemID)
	}
	args := strings.TrimSpace(item.Arguments)
	if args == "" {
		args = strings.TrimSpace(arguments)
	}
	toolName := strings.TrimSpace(item.Name)
	if toolName == "" {
		toolName = strings.TrimSpace(name)
	}
	return fmt.Sprintf(
		"%s(seq=%d resp=%s item_type=%s item_id=%s call_id=%s tool=%s args_len=%d)",
		eventType,
		sequence,
		responseID,
		itemType,
		resolvedItemID,
		callID,
		toolName,
		len(args),
	)
}
