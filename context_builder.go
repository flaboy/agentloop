package agentloop

import (
	"fmt"
	"strings"

	core "github.com/flaboy/agentloop/core"
)

type InboundMessage struct {
	Role    string
	Content string
}

type SystemEvent struct {
	Name    string
	Payload string
}

type ContextBuildRequest struct {
	Inbound                    InboundMessage
	SystemContextJSON          string
	EventContextJSON           string
	ConversationHistory        string
	HistoryMode                HistoryMode
	PreviousResponseID         string
	Store                      *bool
	SystemEvents               []SystemEvent
	ThreadContext              string
	IsFirstTurn                bool
	GroupActivated             bool
	SessionID                  string
	ActorID                    string
	PrebuiltRequest            *core.CreateResponseRequest
	PrebuiltHistoryInputItems  []core.ResponseInputItem
	PrebuiltAppliedHistoryMode HistoryMode
}

type ContextBuildResult struct {
	Request            core.CreateResponseRequest
	HistoryInputItems  []core.ResponseInputItem
	AppliedHistoryMode HistoryMode
}

type ContextBuilder interface {
	Build(req ContextBuildRequest) (ContextBuildResult, error)
}

type DefaultContextBuilder struct{}

func (b DefaultContextBuilder) Build(req ContextBuildRequest) (ContextBuildResult, error) {
	if req.PrebuiltRequest != nil {
		appliedMode := normalizeHistoryMode(req.PrebuiltAppliedHistoryMode)
		if appliedMode == "" {
			appliedMode = HistoryModeLocalReplay
		}
		built := *req.PrebuiltRequest
		built.Input = core.NewResponseInputItems(cloneResponseInputItems(built.Input.Items))
		return ContextBuildResult{
			Request:            built,
			HistoryInputItems:  cloneResponseInputItems(req.PrebuiltHistoryInputItems),
			AppliedHistoryMode: appliedMode,
		}, nil
	}
	role := strings.TrimSpace(req.Inbound.Role)
	if role == "" {
		role = "user"
	}
	inboundContent := strings.TrimSpace(req.Inbound.Content)
	if inboundContent == "" {
		return ContextBuildResult{}, fmt.Errorf("inbound content is required")
	}
	historyInputItems := make([]core.ResponseInputItem, 0, 8)
	systemContextText := strings.TrimSpace(req.SystemContextJSON)
	if systemContextText != "" {
		historyInputItems = append(historyInputItems, buildSystemMessageInputItem(systemContextText))
	}
	appliedMode := resolveHistoryMode(req)
	if appliedMode == HistoryModeLocalReplay {
		historyText := strings.TrimSpace(req.ConversationHistory)
		if historyText != "" {
			historyInputItems = append(historyInputItems, buildSystemMessageInputItem("[Conversation History]\n"+historyText))
		}
	}
	historyInputItems = append(historyInputItems, buildRoleMessageInputItem(role, inboundContent))

	request := core.CreateResponseRequest{
		Input: core.NewResponseInputItems(cloneResponseInputItems(historyInputItems)),
		Store: req.Store,
	}
	if appliedMode == HistoryModeProviderState {
		request.PreviousResponseID = strings.TrimSpace(req.PreviousResponseID)
	}
	return ContextBuildResult{
		Request:            request,
		HistoryInputItems:  historyInputItems,
		AppliedHistoryMode: appliedMode,
	}, nil
}

func resolveHistoryMode(req ContextBuildRequest) HistoryMode {
	mode := normalizeHistoryMode(req.HistoryMode)
	if mode == HistoryModeHybridAuto {
		if req.Store != nil && *req.Store && strings.TrimSpace(req.PreviousResponseID) != "" {
			return HistoryModeProviderState
		}
		return HistoryModeLocalReplay
	}
	if mode == HistoryModeProviderState && strings.TrimSpace(req.PreviousResponseID) == "" {
		return HistoryModeLocalReplay
	}
	return mode
}
