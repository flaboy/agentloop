package core

import "strings"

type ToolError struct {
	ErrorString   string `json:"errorString"`
	SuggestString string `json:"suggestString"`
}

func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.ErrorString) == "" {
		return strings.TrimSpace(e.SuggestString)
	}
	if strings.TrimSpace(e.SuggestString) == "" {
		return strings.TrimSpace(e.ErrorString)
	}
	return strings.TrimSpace(e.ErrorString) + ": " + strings.TrimSpace(e.SuggestString)
}

func NewToolError(errorString, suggestString string) *ToolError {
	return &ToolError{
		ErrorString:   strings.TrimSpace(errorString),
		SuggestString: strings.TrimSpace(suggestString),
	}
}
