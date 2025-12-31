package policy

import (
	"encoding/json"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/subluminal/subluminal/pkg/event"
)

const policyEnvJSON = "SUB_POLICY_JSON"

type Bundle struct {
	Mode  event.RunMode
	Info  event.PolicyInfo
	Rules []Rule
}

type Rule struct {
	RuleID   string         `json:"rule_id"`
	Kind     string         `json:"kind"`
	Enabled  *bool          `json:"enabled"`
	Severity event.Severity `json:"severity"`
	Match    Match          `json:"match"`
	Effect   Effect         `json:"effect"`
}

type Match struct {
	ServerName *NameMatch `json:"server_name,omitempty"`
	ToolName   *NameMatch `json:"tool_name,omitempty"`
}

type NameMatch struct {
	Glob  []string `json:"glob,omitempty"`
	Regex []string `json:"regex,omitempty"`
}

type Effect struct {
	Action     event.DecisionAction `json:"action"`
	ReasonCode string               `json:"reason_code"`
	Message    string               `json:"message"`
}

type Decision struct {
	Action     event.DecisionAction
	RuleID     *string
	ReasonCode string
	Summary    string
	Severity   event.Severity
}

type rawBundle struct {
	Mode          string `json:"mode"`
	PolicyID      string `json:"policy_id"`
	PolicyVersion string `json:"policy_version"`
	PolicyHash    string `json:"policy_hash"`
	Rules         []Rule `json:"rules"`
}

func LoadFromEnv() Bundle {
	raw := strings.TrimSpace(os.Getenv(policyEnvJSON))
	if raw == "" {
		return DefaultBundle()
	}

	var parsed rawBundle
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return DefaultBundle()
	}

	bundle := Bundle{
		Mode: parseMode(parsed.Mode),
		Info: event.PolicyInfo{
			PolicyID:      defaultString(parsed.PolicyID, "default"),
			PolicyVersion: defaultString(parsed.PolicyVersion, "0.1.0"),
			PolicyHash:    defaultString(parsed.PolicyHash, "none"),
		},
		Rules: parsed.Rules,
	}

	if bundle.Mode == "" {
		bundle.Mode = event.RunModeObserve
	}

	return bundle
}

func DefaultBundle() Bundle {
	return Bundle{
		Mode: event.RunModeObserve,
		Info: event.PolicyInfo{
			PolicyID:      "default",
			PolicyVersion: "0.1.0",
			PolicyHash:    "none",
		},
	}
}

func (b Bundle) Decide(serverName, toolName string) Decision {
	for _, rule := range b.Rules {
		if !ruleEnabled(rule.Enabled) {
			continue
		}

		if !matchName(rule.Match.ServerName, serverName) {
			continue
		}
		if !matchName(rule.Match.ToolName, toolName) {
			continue
		}

		action := rule.Effect.Action
		if action == "" {
			action = actionFromKind(rule.Kind)
		}
		if !isValidAction(action) {
			continue
		}

		severity := normalizeSeverity(rule.Severity)
		reason := defaultString(rule.Effect.ReasonCode, defaultReason(action))
		summary := defaultString(rule.Effect.Message, defaultSummary(action))

		if rule.RuleID == "" {
			return Decision{
				Action:     action,
				RuleID:     nil,
				ReasonCode: reason,
				Summary:    summary,
				Severity:   severity,
			}
		}

		ruleID := rule.RuleID
		return Decision{
			Action:     action,
			RuleID:     &ruleID,
			ReasonCode: reason,
			Summary:    summary,
			Severity:   severity,
		}
	}

	return Decision{
		Action:     event.DecisionAllow,
		RuleID:     nil,
		ReasonCode: "DEFAULT_ALLOW",
		Summary:    "Allowed by default policy",
		Severity:   event.SeverityInfo,
	}
}

func ruleEnabled(enabled *bool) bool {
	if enabled == nil {
		return true
	}
	return *enabled
}

func matchName(match *NameMatch, value string) bool {
	if match == nil {
		return true
	}
	if len(match.Glob) == 0 && len(match.Regex) == 0 {
		return true
	}

	for _, glob := range match.Glob {
		if globMatch(glob, value) {
			return true
		}
	}

	for _, pattern := range match.Regex {
		if regexMatch(pattern, value) {
			return true
		}
	}

	return false
}

func globMatch(pattern, value string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	matched, err := path.Match(pattern, value)
	if err != nil {
		return pattern == value
	}
	return matched
}

func regexMatch(pattern, value string) bool {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(value)
}

func actionFromKind(kind string) event.DecisionAction {
	switch strings.ToLower(kind) {
	case "allow":
		return event.DecisionAllow
	case "deny":
		return event.DecisionBlock
	default:
		return ""
	}
}

func normalizeSeverity(severity event.Severity) event.Severity {
	switch severity {
	case event.SeverityInfo, event.SeverityWarn, event.SeverityCritical:
		return severity
	default:
		return event.SeverityInfo
	}
}

func defaultReason(action event.DecisionAction) string {
	switch action {
	case event.DecisionAllow:
		return "POLICY_ALLOW"
	case event.DecisionBlock:
		return "POLICY_BLOCK"
	case event.DecisionThrottle:
		return "POLICY_THROTTLE"
	case event.DecisionRejectWithHint:
		return "POLICY_REJECT"
	case event.DecisionTerminateRun:
		return "POLICY_TERMINATE"
	default:
		return "POLICY_DECISION"
	}
}

func defaultSummary(action event.DecisionAction) string {
	switch action {
	case event.DecisionAllow:
		return "Allowed by policy"
	case event.DecisionBlock:
		return "Blocked by policy"
	case event.DecisionThrottle:
		return "Throttled by policy"
	case event.DecisionRejectWithHint:
		return "Rejected with hint by policy"
	case event.DecisionTerminateRun:
		return "Run terminated by policy"
	default:
		return "Policy decision"
	}
}

func parseMode(mode string) event.RunMode {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "guardrails":
		return event.RunModeGuardrails
	case "control":
		return event.RunModeControl
	default:
		return event.RunModeObserve
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func isValidAction(action event.DecisionAction) bool {
	switch action {
	case event.DecisionAllow, event.DecisionBlock, event.DecisionThrottle,
		event.DecisionRejectWithHint, event.DecisionTerminateRun:
		return true
	default:
		return false
	}
}
