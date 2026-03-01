package agentloop

import "time"

type RunnerState string

const (
	RunnerStateIdle               RunnerState = "idle"
	RunnerStatePreparingContext   RunnerState = "preparing_context"
	RunnerStateCallingModel       RunnerState = "calling_model"
	RunnerStateExecutingTools     RunnerState = "executing_tools"
	RunnerStatePreparingRoundtrip RunnerState = "preparing_roundtrip"
	RunnerStateCompleted          RunnerState = "completed"
	RunnerStateFailed             RunnerState = "failed"
)

type RunnerEvent string

const (
	RunnerEventRunStarted        RunnerEvent = "run_started"
	RunnerEventContextBuilt      RunnerEvent = "context_built"
	RunnerEventModelRequest      RunnerEvent = "model_request"
	RunnerEventModelResponse     RunnerEvent = "model_response"
	RunnerEventToolCallBegin     RunnerEvent = "tool_call_begin"
	RunnerEventToolCallEnd       RunnerEvent = "tool_call_end"
	RunnerEventRoundtripPrepared RunnerEvent = "roundtrip_prepared"
	RunnerEventContextRewritten  RunnerEvent = "context_rewritten"
	RunnerEventRunCompleted      RunnerEvent = "run_completed"
	RunnerEventRunFailed         RunnerEvent = "run_failed"
)

type RunnerSnapshot struct {
	RequestSummary string
	ResponseID     string
	RoundtripMode  string
	ToolCalls      int
	LastError      string
}

type TransitionRecord struct {
	From      RunnerState
	Event     RunnerEvent
	To        RunnerState
	Iteration int
	Timestamp time.Time
	Snapshot  RunnerSnapshot
}
