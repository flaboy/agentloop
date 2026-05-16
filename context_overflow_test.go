package agentloop

import (
	"errors"
	"testing"
)

func TestIsLikelyContextOverflowError_MatchesKnownProviderMessages(t *testing.T) {
	samples := []string{
		"request_too_large",
		"Request exceeds the maximum size",
		"context length exceeded",
		"Maximum context length",
		"prompt is too long: 208423 tokens > 200000 maximum",
		"Context overflow: Summarization failed",
		"413 Request Entity Too Large",
		"Request size exceeds model context window",
		`400 {"type":"error","error":{"type":"invalid_request_error","message":"Request size exceeds model context window"}}`,
		"Context window exceeded: requested 12000 tokens",
		"Prompt too large for this model",
	}
	for _, sample := range samples {
		if !IsLikelyContextOverflowError(errors.New(sample)) {
			t.Fatalf("expected context overflow match for %q", sample)
		}
	}
}

func TestIsLikelyContextOverflowError_RejectsFalsePositives(t *testing.T) {
	samples := []string{
		"rate limit exceeded",
		"too many requests",
		"429 Too Many Requests",
		"exceeded your current quota",
		"request reached organization TPD rate limit, current: 1506556, limit: 1500000",
		"request size exceeds upload limit",
		"model not found",
		"authentication failed",
		"model context window too small: minimum is 1000 tokens",
		"let us investigate the context overflow bug",
	}
	for _, sample := range samples {
		if IsLikelyContextOverflowError(errors.New(sample)) {
			t.Fatalf("unexpected context overflow match for %q", sample)
		}
	}
}
