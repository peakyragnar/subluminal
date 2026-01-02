package mcpstdio

import (
	"regexp"

	"github.com/subluminal/subluminal/pkg/event"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9-_]{6,}`),
	regexp.MustCompile(`ghp_[A-Za-z0-9_]{6,}`),
	regexp.MustCompile(`(?i)password[\w-]+`),
}

func redactSecrets(input string) string {
	if input == "" {
		return input
	}

	redacted := input
	for _, re := range secretPatterns {
		redacted = re.ReplaceAllString(redacted, "[REDACTED]")
	}
	return redacted
}

func sanitizeValue(value any) any {
	switch v := value.(type) {
	case string:
		return redactSecrets(v)
	case map[string]any:
		sanitized := make(map[string]any, len(v))
		for key, val := range v {
			sanitized[key] = sanitizeValue(val)
		}
		return sanitized
	case []any:
		sanitized := make([]any, len(v))
		for i, item := range v {
			sanitized[i] = sanitizeValue(item)
		}
		return sanitized
	default:
		return value
	}
}

func sanitizeHint(hint *event.Hint) *event.Hint {
	if hint == nil {
		return nil
	}

	sanitized := *hint
	sanitized.HintText = redactSecrets(hint.HintText)

	if hint.SuggestedArgs != nil {
		if args, ok := sanitizeValue(hint.SuggestedArgs).(map[string]any); ok {
			sanitized.SuggestedArgs = args
		}
	}

	if hint.RetryAdvice != nil {
		redacted := redactSecrets(*hint.RetryAdvice)
		sanitized.RetryAdvice = &redacted
	}

	return &sanitized
}
