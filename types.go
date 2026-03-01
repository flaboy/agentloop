package agentloop

import (
	"context"
	"encoding/json"

	core "github.com/flaboy/agentloop/core"
)

type ResponseToolSpec = core.ResponseToolSpec

type Tool interface {
	Name() string
	Spec() ResponseToolSpec
	Execute(ctx context.Context, input json.RawMessage, callID string) (string, *ToolError)
}
