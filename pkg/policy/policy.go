package policy

import (
	"encoding/json"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/subluminal/subluminal/pkg/event"
)

const policyEnvJSON = "SUB_POLICY_JSON"

type Bundle struct {
	Mode  event.RunMode
	Info  event.PolicyInfo
	Rules []Rule

	breakerState map[string][]time.Time
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
	Breaker    *BreakerEffect       `json:"breaker,omitempty"`
}

type BreakerEffect struct {
	Scope           string `json:"scope"`
	ErrorThreshold  int    `json:"error_threshold"`
	WindowMS        int    `json:"window_ms"`
	RepeatThreshold int    `json:"repeat_threshold"`
	RepeatWindowMS  int    `json:"repeat_window_ms"`
	OnTrip          string `json:"on_trip"`
	TerminateCode   string `json:"terminate_code"`
	HintText        string `json:"hint_text"`
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

	bundle.initState()
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
		breakerState: make(map[string][]time.Time),
	}
}

func (b *Bundle) Decide(serverName, toolName, argsHash string) Decision {
	b.initState()

	now := time.Now()
	var orderedDecision *Decision
	var breakerDecision *Decision

	for i, rule := range b.Rules {
		if !ruleEnabled(rule.Enabled) {
			continue
		}

		if !matchName(rule.Match.ServerName, serverName) {
			continue
		}
		if !matchName(rule.Match.ToolName, toolName) {
			continue
		}

		kind := strings.ToLower(strings.TrimSpace(rule.Kind))
		if kind == "breaker" {
			breaker := rule.Effect.Breaker
			if breaker == nil {
				continue
			}
			if breaker.RepeatThreshold <= 0 || breaker.RepeatWindowMS <= 0 {
				continue
			}

			key := breakerKey(breakerRuleKey(rule, i), breaker.Scope, serverName, toolName, argsHash)
			count := b.recordRepeat(key, now, time.Duration(breaker.RepeatWindowMS)*time.Millisecond)
			if count >= breaker.RepeatThreshold && breakerDecision == nil {
				action := breakerAction(breaker.OnTrip)
				breakerDecision = buildDecision(rule, action, "BREAKER_TRIPPED", "Breaker tripped")
			}
			continue
		}

		action := rule.Effect.Action
		if action == "" {
			action = actionFromKind(kind)
		}
		if action != event.DecisionAllow && action != event.DecisionBlock {
			continue
		}
		if orderedDecision == nil {
			orderedDecision = buildDecision(rule, action, defaultReason(action), defaultSummary(action))
		}
	}

	if breakerDecision != nil {
		return *breakerDecision
	}
	if orderedDecision != nil {
		return *orderedDecision
	}
	return Decision{
		Action:     event.DecisionAllow,
		RuleID:     nil,
		ReasonCode: "DEFAULT_ALLOW",
		Summary:    "Allowed by default policy",
		Severity:   event.SeverityInfo,
	}
}

func (b *Bundle) initState() {
	if b.breakerState == nil {
		b.breakerState = make(map[string][]time.Time)
	}
}

func (b *Bundle) recordRepeat(key string, now time.Time, window time.Duration) int {
	if window <= 0 {
		return 0
	}

	hits := b.breakerState[key]
	cutoff := now.Add(-window)
	kept := hits[:0]
	for _, ts := range hits {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	kept = append(kept, now)
	b.breakerState[key] = kept
	return len(kept)
}

func breakerRuleKey(rule Rule, index int) string {
	if strings.TrimSpace(rule.RuleID) != "" {
		return rule.RuleID
	}
	return "rule-" + strconv.Itoa(index)
}

func breakerKey(ruleKey, scope, serverName, toolName, argsHash string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "run":
		return strings.Join([]string{ruleKey, "run", argsHash}, "|")
	case "tool":
		return strings.Join([]string{ruleKey, "tool", toolName, argsHash}, "|")
	case "server_tool":
		return strings.Join([]string{ruleKey, "server_tool", serverName, toolName, argsHash}, "|")
	default:
		return strings.Join([]string{ruleKey, "server_tool", serverName, toolName, argsHash}, "|")
	}
}

func breakerAction(onTrip string) event.DecisionAction {
	switch strings.ToUpper(strings.TrimSpace(onTrip)) {
	case "TERMINATE_RUN":
		return event.DecisionTerminateRun
	case "BLOCK":
		return event.DecisionBlock
	default:
		return event.DecisionBlock
	}
}

func buildDecision(rule Rule, action event.DecisionAction, fallbackReason, fallbackSummary string) *Decision {
	severity := normalizeSeverity(rule.Severity)
	reason := defaultString(rule.Effect.ReasonCode, fallbackReason)
	summary := defaultString(rule.Effect.Message, fallbackSummary)

	if strings.TrimSpace(rule.RuleID) == "" {
		return &Decision{
			Action:     action,
			RuleID:     nil,
			ReasonCode: reason,
			Summary:    summary,
			Severity:   severity,
		}
	}

	ruleID := rule.RuleID
	return &Decision{
		Action:     action,
		RuleID:     &ruleID,
		ReasonCode: reason,
		Summary:    summary,
		Severity:   severity,
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
	case event.DecisionBlock:
		return "POLICY_BLOCK"
	case event.DecisionAllow:
		return "POLICY_ALLOW"
	default:
		return "POLICY_DECISION"
	}
}

func defaultSummary(action event.DecisionAction) string {
	switch action {
	case event.DecisionBlock:
		return "Blocked by policy"
	case event.DecisionAllow:
		return "Allowed by policy"
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
