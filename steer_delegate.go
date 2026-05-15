package agentloop

import (
	"context"
	"time"

	core "github.com/flaboy/agentloop/core"
)

type SteerBoundary string

const (
	SteerBoundaryBeforeModelCall               SteerBoundary = "before_model_call"
	SteerBoundaryAfterModelResponseBeforeFinal SteerBoundary = "after_model_response_before_final"
	SteerBoundaryAfterToolRoundtrip            SteerBoundary = "after_tool_roundtrip"
)

type SteerEvent struct {
	ID         string
	SourceType string
	IntentType string
	Content    string
	CreatedAt  time.Time
}

type SteerDelegateInput struct {
	Iteration              int
	OriginalContextRequest ContextBuildRequest
	CurrentRequest         core.CreateResponseRequest
	Response               *core.CreateResponseResult
	AppliedHistoryMode     HistoryMode
	Boundary               SteerBoundary
}

type SteerDelegateOutput struct {
	Events                []SteerEvent
	RewriteRequest        *ContextBuildRequest
	ForceHistoryMode      HistoryMode
	ResetPreviousResponse bool
	Stop                  bool
	Reason                string
}

type SteerDelegate func(context.Context, SteerDelegateInput) (SteerDelegateOutput, error)
