package agentloop

import (
	"encoding/json"

	core "github.com/flaboy/agentloop/core"
)

type ToolError = core.ToolError

func NewToolError(errorString, suggestString string) *ToolError {
	if suggestString == "" {
		suggestString = "NO_SUGGESTION"
	}
	return core.NewToolError(errorString, suggestString)
}

func mustMarshalToolError(err *ToolError) string {
	if err == nil {
		err = NewToolError("UNKNOWN_ERROR", "NO_SUGGESTION")
	}
	raw, _ := json.Marshal(err)
	return string(raw)
}
