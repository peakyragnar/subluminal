package mcpstdio

import (
	"regexp"
	"strings"

	"github.com/subluminal/subluminal/pkg/event"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9-_]{6,}`),
	regexp.MustCompile(`ghp_[A-Za-z0-9_]{6,}`),
	regexp.MustCompile(`(?i)password[\w-]+`),
}

// Redactor removes known secret patterns and injected secret values.
type Redactor struct {
	patterns []*regexp.Regexp
	literals []string
}

// NewRedactor builds a redactor with optional literal secret values.
func NewRedactor(literals []string) *Redactor {
	filtered := make([]string, 0, len(literals))
	for _, literal := range literals {
		if literal == "" {
			continue
		}
		filtered = append(filtered, literal)
	}
	return &Redactor{
		patterns: secretPatterns,
		literals: filtered,
	}
}

// Redact replaces secret values in the input string.
func (r *Redactor) Redact(input string) string {
	if input == "" {
		return input
	}

	redacted := input
	for _, literal := range r.literals {
		redacted = strings.ReplaceAll(redacted, literal, "[REDACTED]")
	}
	for _, re := range r.patterns {
		redacted = re.ReplaceAllString(redacted, "[REDACTED]")
	}
	return redacted
}

// SanitizeValue recursively redacts secrets from structured data.
func (r *Redactor) SanitizeValue(value any) any {
	switch v := value.(type) {
	case string:
		return r.Redact(v)
	case map[string]any:
		sanitized := make(map[string]any, len(v))
		for key, val := range v {
			sanitized[key] = r.SanitizeValue(val)
		}
		return sanitized
	case []any:
		sanitized := make([]any, len(v))
		for i, item := range v {
			sanitized[i] = r.SanitizeValue(item)
		}
		return sanitized
	default:
		return value
	}
}

// SanitizeHint redacts secret values from hint content.
func (r *Redactor) SanitizeHint(hint *event.Hint) *event.Hint {
	if hint == nil {
		return nil
	}

	sanitized := *hint
	sanitized.HintText = r.Redact(hint.HintText)

	if hint.SuggestedArgs != nil {
		if args, ok := r.SanitizeValue(hint.SuggestedArgs).(map[string]any); ok {
			sanitized.SuggestedArgs = args
		}
	}

	if hint.RetryAdvice != nil {
		redacted := r.Redact(*hint.RetryAdvice)
		sanitized.RetryAdvice = &redacted
	}

	return &sanitized
}

var defaultRedactor = NewRedactor(nil)

func redactSecrets(input string) string {
	return defaultRedactor.Redact(input)
}

func sanitizeValue(value any) any {
	return defaultRedactor.SanitizeValue(value)
}

func sanitizeHint(hint *event.Hint) *event.Hint {
	return defaultRedactor.SanitizeHint(hint)
}
