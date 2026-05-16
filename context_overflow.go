package agentloop

import (
	"regexp"
	"strings"
)

var (
	contextWindowTooSmallRE = regexp.MustCompile(`(?i)context window.*(too small|minimum is)`)
	contextOverflowHintRE   = regexp.MustCompile(`(?i)context window.*(too (?:large|long)|exceed|over|limit|max(?:imum)?|requested|sent|tokens)|prompt.*(too (?:large|long)|exceed|over|limit|max(?:imum)?)|(?:request|input).*(?:context|window|length|token).*(too (?:large|long)|exceed|over|limit|max(?:imum)?)`)
	rateLimitHintRE         = regexp.MustCompile(`(?i)rate limit|too many requests|requests per (?:minute|hour|day)|quota|throttl|429\b`)
)

func IsLikelyContextOverflowError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return false
	}
	if contextWindowTooSmallRE.MatchString(msg) {
		return false
	}
	if isLikelyRateLimitText(msg) {
		return false
	}
	if isExplicitContextOverflowText(msg) {
		return true
	}
	if rateLimitHintRE.MatchString(msg) {
		return false
	}
	return contextOverflowHintRE.MatchString(msg)
}

func isExplicitContextOverflowText(msg string) bool {
	lower := strings.ToLower(msg)
	hasRequestSizeExceeds := strings.Contains(lower, "request size exceeds")
	hasContextWindow := strings.Contains(lower, "context window") ||
		strings.Contains(lower, "context length") ||
		strings.Contains(lower, "maximum context length")
	return strings.Contains(lower, "request_too_large") ||
		strings.Contains(lower, "request exceeds the maximum size") ||
		strings.Contains(lower, "context length exceeded") ||
		strings.Contains(lower, "maximum context length") ||
		strings.Contains(lower, "prompt is too long") ||
		strings.Contains(lower, "exceeds model context window") ||
		(hasRequestSizeExceeds && hasContextWindow) ||
		strings.Contains(lower, "context overflow:") ||
		(strings.Contains(lower, "413") && strings.Contains(lower, "too large"))
}

func isLikelyRateLimitText(msg string) bool {
	return rateLimitHintRE.MatchString(msg)
}
