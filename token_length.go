package agentloop

import (
	"strings"

	core "github.com/flaboy/agentloop/core"
)

type TokenLengthEstimator func(core.CreateResponseRequest) int64

func (r *LoopRunner) estimateRequestTokenLength(req core.CreateResponseRequest) int64 {
	if r != nil && r.options.TokenLengthEstimator != nil {
		if n := r.options.TokenLengthEstimator(req); n > 0 {
			return n
		}
	}
	return ApproximateRequestTokenLength(req)
}

func ApproximateRequestTokenLength(req core.CreateResponseRequest) int64 {
	textChars := 0
	textChars += len(strings.TrimSpace(req.Model))
	textChars += len(strings.TrimSpace(req.Input.Text))
	textChars += len(strings.TrimSpace(req.PreviousResponseID))
	for _, tool := range req.Tools {
		textChars += len(strings.TrimSpace(tool.Type))
		textChars += len(strings.TrimSpace(tool.Name))
		textChars += len(strings.TrimSpace(tool.Description))
		textChars += len(strings.TrimSpace(tool.Parameters.Type))
		for _, required := range tool.Parameters.Required {
			textChars += len(strings.TrimSpace(required))
		}
		for _, property := range tool.Parameters.Properties {
			textChars += len(strings.TrimSpace(property.Name))
			textChars += len(strings.TrimSpace(property.Schema.Type))
			for _, enum := range property.Schema.Enum {
				textChars += len(strings.TrimSpace(enum))
			}
		}
	}
	for _, item := range req.Input.Items {
		textChars += len(strings.TrimSpace(item.Type))
		textChars += len(strings.TrimSpace(item.Role))
		textChars += len(strings.TrimSpace(item.CallID))
		textChars += len(strings.TrimSpace(item.Output))
		textChars += len(strings.TrimSpace(item.ID))
		textChars += len(strings.TrimSpace(item.Name))
		textChars += len(strings.TrimSpace(item.Arguments))
		for _, part := range item.Content {
			textChars += len(strings.TrimSpace(part.Type))
			textChars += len(strings.TrimSpace(part.Text))
			textChars += len(strings.TrimSpace(part.ImageURL))
		}
	}
	if textChars <= 0 {
		return 0
	}
	return int64((textChars + 3) / 4)
}
