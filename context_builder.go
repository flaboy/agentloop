package agentloop

import (
	"fmt"
	"strings"

	core "github.com/flaboy/agentloop/core"
)

type ContextBuildRequest struct {
	UserPrompt   string
	StoreEnabled bool
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
	userPrompt := strings.TrimSpace(req.UserPrompt)
	if userPrompt == "" {
		return ContextBuildResult{}, fmt.Errorf("user prompt is required")
	}
	userPromptForUserMessage := userPrompt
	historyInputItems := make([]core.ResponseInputItem, 0, 8)
	if systemContextText, ok := extractPromptSystemContextJSON(userPrompt); ok {
		historyInputItems = append(historyInputItems, buildSystemMessageInputItem(systemContextText))
		if stripped, ok := stripPromptSystemContextSection(userPrompt); ok {
			userPromptForUserMessage = stripped
		}
	}
	historyInputItems = append(historyInputItems, buildUserMessageInputItem(userPromptForUserMessage))

	request := core.CreateResponseRequest{
		Store: boolPtr(req.StoreEnabled),
		Input: core.NewResponseInputItems(cloneResponseInputItems(historyInputItems)),
	}
	return ContextBuildResult{
		Request:           request,
		HistoryInputItems: historyInputItems,
	}, nil
}
