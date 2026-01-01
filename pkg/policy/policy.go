package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/subluminal/subluminal/pkg/event"
)

const policyEnvJSON = "SUB_POLICY_JSON"

type Bundle struct {
	Mode  event.RunMode
	Info  event.PolicyInfo
	Rules []Rule

	budgets   *budgetState
	rateLimit *rateLimitState
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
	RateLimit  *RateLimitEffect     `json:"rate_limit,omitempty"`
}

type RateLimitEffect struct {
	Scope             string               `json:"scope"`
	Capacity          int                  `json:"capacity"`
	RefillTokens      int                  `json:"refill_tokens"`
	RefillPeriodMS    int                  `json:"refill_period_ms"`
	CostTokensPerCall int                  `json:"cost_tokens_per_call"`
	OnLimit           event.DecisionAction `json:"on_limit"`
	BackoffMS         int                  `json:"backoff_ms"`
	HintText          string               `json:"hint_text"`
}

type BudgetEffect struct {
	Scope            string               `json:"scope"`
	LimitCalls       *int                 `json:"limit_calls,omitempty"`
	LimitCostUnits   *int                 `json:"limit_cost_units,omitempty"`
	CostUnitsPerCall *int                 `json:"cost_units_per_call,omitempty"`
	OnExceed         event.DecisionAction `json:"on_exceed"`
	HintText         string               `json:"hint_text,omitempty"`
}

type Decision struct {
	Action     event.DecisionAction
	RuleID     *string
	ReasonCode string
	Summary    string
	Severity   event.Severity
	BackoffMS  int
}

type rateLimitConfig struct {
	Scope             string
	Capacity          int
	RefillTokens      int
	RefillPeriod      time.Duration
	CostTokensPerCall int
	OnLimit           event.DecisionAction
	BackoffMS         int
}

type rateLimitState struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

type tokenBucket struct {
	tokens     int
	lastRefill time.Time
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
		Rules:     parsed.Rules,
		budgets:   newBudgetState(),
		rateLimit: newRateLimitState(),
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
		budgets:   newBudgetState(),
		rateLimit: newRateLimitState(),
	}
}

func (b *Bundle) Decide(serverName, toolName string) Decision {
	b.ensureState()

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

		// Check budget rules (POL-003)
		if isBudgetRule(rule) {
			budgetDecision := b.applyBudgetDecision(rule, idx, serverName, toolName)
			if budgetDecision != nil && decision == nil {
				decision = budgetDecision
			}
			continue
		}

		// Check rate limit rules (POL-004)
		if rule.Effect.RateLimit != nil {
			rateLimitDec, limited := b.rateLimitDecision(idx, rule, serverName, toolName)
			if limited {
				return rateLimitDec
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

func (b *Bundle) ensureState() {
	if b.budgets == nil {
		b.budgets = newBudgetState()
	}
	if b.rateLimit == nil {
		b.rateLimit = newRateLimitState()
	}
}

func isBudgetRule(rule Rule) bool {
	if rule.Effect.Budget != nil {
		return true
	}
	return strings.EqualFold(rule.Kind, "budget")
}

func (b *Bundle) applyBudgetDecision(rule Rule, ruleIndex int, serverName, toolName string) *Decision {
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

func (b *Bundle) rateLimitDecision(ruleIndex int, rule Rule, serverName, toolName string) (Decision, bool) {
	config := normalizeRateLimit(rule.Effect.RateLimit)
	if b.rateLimit.allow(ruleIndex, config, serverName, toolName) {
		return Decision{}, false
	}

	action := config.OnLimit
	severity := normalizeSeverity(rule.Severity)
	reason := defaultString(rule.Effect.ReasonCode, defaultReason(action))
	summary := defaultString(rule.Effect.Message, defaultSummary(action))

	decision := Decision{
		Action:     action,
		RuleID:     nil,
		ReasonCode: reason,
		Summary:    summary,
		Severity:   severity,
	}
	if action == event.DecisionThrottle {
		decision.BackoffMS = config.BackoffMS
	}

	if rule.RuleID == "" {
		return decision, true
	}

	ruleID := rule.RuleID
	decision.RuleID = &ruleID
	return decision, true
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
	case event.DecisionThrottle:
		return "POLICY_THROTTLED"
	case event.DecisionRejectWithHint:
		return "POLICY_HINT"
	case event.DecisionTerminateRun:
		return "POLICY_TERMINATED"
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
	case event.DecisionThrottle:
		return "Throttled by policy"
	case event.DecisionRejectWithHint:
		return "Rejected with hint"
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

func normalizeRateLimit(effect *RateLimitEffect) rateLimitConfig {
	config := rateLimitConfig{
		Scope:             strings.ToLower(strings.TrimSpace(effect.Scope)),
		Capacity:          effect.Capacity,
		RefillTokens:      effect.RefillTokens,
		RefillPeriod:      time.Duration(effect.RefillPeriodMS) * time.Millisecond,
		CostTokensPerCall: effect.CostTokensPerCall,
		OnLimit:           normalizeOnLimitAction(effect.OnLimit),
		BackoffMS:         effect.BackoffMS,
	}

	if config.Scope == "" {
		config.Scope = "run"
	}
	if config.Capacity < 0 {
		config.Capacity = 0
	}
	if config.RefillTokens < 0 {
		config.RefillTokens = 0
	}
	if config.RefillPeriod < 0 {
		config.RefillPeriod = 0
	}
	if config.CostTokensPerCall <= 0 {
		config.CostTokensPerCall = 1
	}
	if config.OnLimit == "" {
		config.OnLimit = event.DecisionThrottle
	}
	// Default backoff_ms when throttling - clients need to know how long to wait (Interface-Pack §2.5, ERR-002)
	if config.OnLimit == event.DecisionThrottle && config.BackoffMS <= 0 {
		config.BackoffMS = 1000
	}

	return config
}

func normalizeOnLimitAction(action event.DecisionAction) event.DecisionAction {
	switch strings.ToUpper(string(action)) {
	case string(event.DecisionThrottle):
		return event.DecisionThrottle
	case string(event.DecisionBlock):
		return event.DecisionBlock
	case string(event.DecisionRejectWithHint):
		return event.DecisionRejectWithHint
	default:
		return ""
	}
}

func newRateLimitState() *rateLimitState {
	return &rateLimitState{
		buckets: make(map[string]*tokenBucket),
	}
}

func (state *rateLimitState) allow(ruleIndex int, config rateLimitConfig, serverName, toolName string) bool {
	key := rateLimitKey(ruleIndex, config.Scope, serverName, toolName)
	now := time.Now()

	state.mu.Lock()
	bucket := state.buckets[key]
	if bucket == nil {
		bucket = newTokenBucket(config.Capacity, now)
		state.buckets[key] = bucket
	}
	allowed := bucket.allow(now, config)
	state.mu.Unlock()

	return allowed
}

func rateLimitKey(ruleIndex int, scope, serverName, toolName string) string {
	switch strings.ToLower(scope) {
	case "server_tool":
		return fmt.Sprintf("rate_limit:%d:server_tool:%s:%s", ruleIndex, serverName, toolName)
	case "tool":
		return fmt.Sprintf("rate_limit:%d:tool:%s", ruleIndex, toolName)
	default:
		return fmt.Sprintf("rate_limit:%d:run", ruleIndex)
	}
}

func newTokenBucket(capacity int, now time.Time) *tokenBucket {
	if capacity < 0 {
		capacity = 0
	}
	return &tokenBucket{
		tokens:     capacity,
		lastRefill: now,
	}
}

func (bucket *tokenBucket) allow(now time.Time, config rateLimitConfig) bool {
	bucket.refill(now, config)
	if bucket.tokens < config.CostTokensPerCall {
		return false
	}
	bucket.tokens -= config.CostTokensPerCall
	return true
}

func (bucket *tokenBucket) refill(now time.Time, config rateLimitConfig) {
	if config.RefillTokens <= 0 || config.RefillPeriod <= 0 {
		return
	}

	elapsed := now.Sub(bucket.lastRefill)
	if elapsed < config.RefillPeriod {
		return
	}

	periods := int(elapsed / config.RefillPeriod)
	if periods <= 0 {
		return
	}

	tokens := bucket.tokens + (periods * config.RefillTokens)
	if tokens > config.Capacity {
		tokens = config.Capacity
	}
	bucket.tokens = tokens
	bucket.lastRefill = bucket.lastRefill.Add(time.Duration(periods) * config.RefillPeriod)
}
