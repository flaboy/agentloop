package agentloop

import "time"

type LoopEvent interface {
	LoopEventType() string
}

const (
	LoopEventTypeModelRequest     = "model_request"
	LoopEventTypeModelResponse    = "model_response"
	LoopEventTypeToolInput        = "tool_input"
	LoopEventTypeToolOutput       = "tool_output"
	LoopEventTypeRoundtripPrepare = "roundtrip_prepared"
	LoopEventTypeContextRewrite   = "context_rewrite"
	LoopEventTypeTransition       = "runner_transition"
)

type ModelRequestEvent struct {
	Iteration        int
	Timestamp        time.Time
	Request          string
	PreviousResponse string
	RoundtripMode    string
}

func (ModelRequestEvent) LoopEventType() string { return LoopEventTypeModelRequest }

type ModelResponseEvent struct {
	Iteration        int
	Timestamp        time.Time
	ResponseID       string
	ToolCalls        int
	ToolCallsSummary string
	FinalTextLen     int
	EventTrace       string
	EventCount       int
}

func (ModelResponseEvent) LoopEventType() string { return LoopEventTypeModelResponse }

type ToolInputEvent struct {
	Iteration    int
	Timestamp    time.Time
	CallID       string
	ResponseID   string
	ToolName     string
	Input        string
	InputRawLen  int
	InputPreview string
}

func (ToolInputEvent) LoopEventType() string { return LoopEventTypeToolInput }

type ToolOutputEvent struct {
	Iteration   int
	Timestamp   time.Time
	CallID      string
	ResponseID  string
	ToolName    string
	State       string
	ErrorString string
	OutputLen   int
	Output      string
}

func (ToolOutputEvent) LoopEventType() string { return LoopEventTypeToolOutput }

type RoundtripPreparedEvent struct {
	Iteration          int
	Timestamp          time.Time
	PreviousResponseID string
	RoundtripMode      string
	ItemsCount         int
	ItemsSummary       string
}

func (RoundtripPreparedEvent) LoopEventType() string { return LoopEventTypeRoundtripPrepare }

type ContextRewriteEvent struct {
	Iteration           int
	Timestamp           time.Time
	ClearReasons        []string
	PreviousRoundMode   string
	CurrentRoundMode    string
	InitialCurrentCmd   string
	HistoryItemsUpdated bool
}

func (ContextRewriteEvent) LoopEventType() string { return LoopEventTypeContextRewrite }

type TransitionEvent struct {
	Record TransitionRecord
}

func (TransitionEvent) LoopEventType() string { return LoopEventTypeTransition }
