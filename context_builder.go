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
	Inbound             InboundMessage
	SystemContextJSON   string
	EventContextJSON    string
	ConversationHistory string
	SystemEvents        []SystemEvent
	ThreadContext       string
	IsFirstTurn         bool
	GroupActivated      bool
	SessionID           string
	ActorID             string
}

type ContextBuildResult struct {
	Request           core.CreateResponseRequest
	HistoryInputItems []core.ResponseInputItem
}

type ContextBuilder interface {
	Build(req ContextBuildRequest) (ContextBuildResult, error)
}

type DefaultContextBuilder struct{}

func (b DefaultContextBuilder) Build(req ContextBuildRequest) (ContextBuildResult, error) {
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
	historyInputItems = append(historyInputItems, buildRoleMessageInputItem(role, inboundContent))

	request := core.CreateResponseRequest{
		Input: core.NewResponseInputItems(cloneResponseInputItems(historyInputItems)),
	}
	return ContextBuildResult{
		Request:           request,
		HistoryInputItems: historyInputItems,
	}, nil
}
