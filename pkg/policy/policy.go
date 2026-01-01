package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/subluminal/subluminal/pkg/event"
)

const policyEnvJSON = "SUB_POLICY_JSON"

type Bundle struct {
	Mode  event.RunMode
	Info  event.PolicyInfo
	Rules []Rule

	budgets *budgetState
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
	Budget     *BudgetEffect        `json:"budget,omitempty"`
}

type Decision struct {
	Action     event.DecisionAction
	RuleID     *string
	ReasonCode string
	Summary    string
	Severity   event.Severity
}

type BudgetEffect struct {
	Scope            string               `json:"scope"`
	LimitCalls       *int                 `json:"limit_calls,omitempty"`
	LimitCostUnits   *int                 `json:"limit_cost_units,omitempty"`
	CostUnitsPerCall *int                 `json:"cost_units_per_call,omitempty"`
	OnExceed         event.DecisionAction `json:"on_exceed"`
	HintText         string               `json:"hint_text,omitempty"`
}

type budgetState struct {
	mu    sync.Mutex
	calls map[string]int
}

func newBudgetState() *budgetState {
	return &budgetState{
		calls: make(map[string]int),
	}
}

func (bs *budgetState) incrementCalls(key string, delta int) int {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.calls[key] += delta
	return bs.calls[key]
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
		Rules:   parsed.Rules,
		budgets: newBudgetState(),
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
		budgets: newBudgetState(),
	}
}

func (b Bundle) Decide(serverName, toolName string) Decision {
	var decision *Decision
	for idx, rule := range b.Rules {
		if !ruleEnabled(rule.Enabled) {
			continue
		}

		if !matchName(rule.Match.ServerName, serverName) {
			continue
		}
		if !matchName(rule.Match.ToolName, toolName) {
			continue
		}

		if isBudgetRule(rule) {
			budgetDecision := b.applyBudgetDecision(rule, idx, serverName, toolName)
			if budgetDecision != nil && decision == nil {
				decision = budgetDecision
			}
			continue
		}

		action := rule.Effect.Action
		if action == "" {
			action = actionFromKind(rule.Kind)
		}
		if action != event.DecisionAllow && action != event.DecisionBlock {
			continue
		}

		severity := normalizeSeverity(rule.Severity)
		reason := defaultString(rule.Effect.ReasonCode, defaultReason(action))
		summary := defaultString(rule.Effect.Message, defaultSummary(action))

		dec := decisionForRule(rule, action, reason, summary, severity)
		if decision == nil {
			decision = &dec
		}
	}

	if decision != nil {
		return *decision
	}

	return Decision{
		Action:     event.DecisionAllow,
		RuleID:     nil,
		ReasonCode: "DEFAULT_ALLOW",
		Summary:    "Allowed by default policy",
		Severity:   event.SeverityInfo,
	}
}

func isBudgetRule(rule Rule) bool {
	if rule.Effect.Budget != nil {
		return true
	}
	return strings.EqualFold(rule.Kind, "budget")
}

func (b Bundle) applyBudgetDecision(rule Rule, ruleIndex int, serverName, toolName string) *Decision {
	if rule.Effect.Budget == nil {
		return nil
	}

	limit := rule.Effect.Budget.LimitCalls
	if limit == nil {
		return nil
	}

	bs := b.budgets
	if bs == nil {
		bs = newBudgetState()
	}

	scope := strings.TrimSpace(strings.ToLower(rule.Effect.Budget.Scope))
	key := budgetKey(rule.RuleID, ruleIndex, scope, serverName, toolName)
	count := bs.incrementCalls(key, 1)

	if count <= *limit {
		return nil
	}

	action := rule.Effect.Budget.OnExceed
	if action == "" {
		action = event.DecisionBlock
	}
	reason := defaultString(rule.Effect.ReasonCode, "BUDGET_EXCEEDED")
	summary := defaultString(rule.Effect.Message, "Budget exceeded")
	severity := normalizeSeverity(rule.Severity)

	dec := decisionForRule(rule, action, reason, summary, severity)
	return &dec
}

func budgetKey(ruleID string, ruleIndex int, scope, serverName, toolName string) string {
	base := ruleID
	if strings.TrimSpace(base) == "" {
		base = fmt.Sprintf("budget:%d", ruleIndex)
	}

	switch scope {
	case "", "run":
		return fmt.Sprintf("%s|run", base)
	case "tool":
		return fmt.Sprintf("%s|tool:%s", base, toolName)
	case "server_tool":
		return fmt.Sprintf("%s|server_tool:%s:%s", base, serverName, toolName)
	default:
		return fmt.Sprintf("%s|run", base)
	}
}

func decisionForRule(rule Rule, action event.DecisionAction, reason, summary string, severity event.Severity) Decision {
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
