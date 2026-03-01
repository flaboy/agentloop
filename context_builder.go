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

func BuildContextRequestFromPrompt(userPrompt string) (ContextBuildRequest, error) {
	prompt := strings.TrimSpace(userPrompt)
	if prompt == "" {
		return ContextBuildRequest{}, fmt.Errorf("user prompt is required")
	}
	systemContext := ""
	if extracted, ok := extractPromptSystemContextJSON(prompt); ok {
		systemContext = extracted
		if stripped, found := stripPromptSystemContextSection(prompt); found {
			prompt = stripped
		}
	}
	return ContextBuildRequest{
		Inbound: InboundMessage{
			Role:    "user",
			Content: prompt,
		},
		SystemContextJSON: systemContext,
	}, nil
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
