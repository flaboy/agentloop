package core

import "strings"

type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Code) == "" {
		return strings.TrimSpace(e.Message)
	}
	if strings.TrimSpace(e.Message) == "" {
		return strings.TrimSpace(e.Code)
	}
	return strings.TrimSpace(e.Code) + ": " + strings.TrimSpace(e.Message)
}

func NewToolError(code, message string) *ToolError {
	return &ToolError{Code: strings.TrimSpace(code), Message: strings.TrimSpace(message)}
}
