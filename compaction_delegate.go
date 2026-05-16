package agentloop

import core "github.com/flaboy/agentloop/core"

type CompactionTrigger string

const (
	CompactionTriggerThreshold       CompactionTrigger = "threshold"
	CompactionTriggerContextOverflow CompactionTrigger = "context_overflow"
)

type CompactionDelegateInput struct {
	Iteration                 int
	OriginalContextRequest    ContextBuildRequest
	CurrentRequest            core.CreateResponseRequest
	Response                  core.CreateResponseResult
	ReplayItems               []core.ResponseInputItem
	AppliedHistoryMode        HistoryMode
	PreviousResponseID        string
	Trigger                   CompactionTrigger
	ContextTokens             int64
	ContextOverflowErrorText  string
	ContextOverflowRetryCount int
}

type CompactionDelegateOutput struct {
	NeedCompaction        bool
	RewriteRequest        *ContextBuildRequest
	ForceHistoryMode      HistoryMode
	ResetPreviousResponse bool
	Reason                string
}

type CompactionDelegate func(CompactionDelegateInput) (CompactionDelegateOutput, error)
