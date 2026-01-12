package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/peakyragnar/subluminal/pkg/event"
)

const policyEnvJSON = "SUB_POLICY_JSON"

// debugPolicy enables verbose logging for policy debugging.
// Set SUB_POLICY_DEBUG=1 to enable.
var debugPolicy = os.Getenv("SUB_POLICY_DEBUG") == "1"

func debugLog(format string, args ...any) {
	if debugPolicy {
		fmt.Fprintf(os.Stderr, "[POLICY DEBUG] "+format+"\n", args...)
	}
}

type Bundle struct {
	Mode      event.RunMode
	Info      event.PolicyInfo
	Defaults  PolicyDefaults
	Selectors PolicySelectors
	Rules     []Rule

	breakerMu    sync.Mutex
	breakerState map[string][]time.Time
	budgets      *budgetState
	rateLimit    *rateLimitState
	dedupe       *dedupeCache
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
	RiskClass  []string   `json:"risk_class,omitempty"`
	Args       *ArgsMatch `json:"args,omitempty"`
}

type NameMatch struct {
	Glob  []string `json:"glob,omitempty"`
	Regex []string `json:"regex,omitempty"`
}

type Effect struct {
	Action     event.DecisionAction `json:"action"`
	ReasonCode string               `json:"reason_code"`
	Message    string               `json:"message"`
	Hint       *event.Hint          `json:"hint,omitempty"`
	Breaker    *BreakerEffect       `json:"breaker,omitempty"`
	Budget     *BudgetEffect        `json:"budget,omitempty"`
	RateLimit  *RateLimitEffect     `json:"rate_limit,omitempty"`
	Dedupe     *DedupeEffect        `json:"dedupe,omitempty"`
	Tag        *TagEffect           `json:"tag,omitempty"`
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

type DedupeEffect struct {
	Scope       string               `json:"scope"`
	WindowMS    int                  `json:"window_ms"`
	Key         string               `json:"key"`
	OnDuplicate event.DecisionAction `json:"on_duplicate"`
	HintText    string               `json:"hint_text"`
}

type TagEffect struct {
	AddRiskClass []string `json:"add_risk_class"`
}

// DecisionContext provides inputs for policy evaluation.
type DecisionContext struct {
	ServerName string
	ToolName   string
	ArgsHash   string
	Args       map[string]any
	Target     SelectorTarget
}

type Decision struct {
	Action     event.DecisionAction
	RuleID     *string
	ReasonCode string
	Summary    string
	Severity   event.Severity
	BackoffMS  int
	Hint       *event.Hint
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

type dedupeCache struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

func newDedupeCache() *dedupeCache {
	return &dedupeCache{
		entries: make(map[string]time.Time),
	}
}

func (d *dedupeCache) IsDuplicate(key string, window time.Duration, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	lastSeen, ok := d.entries[key]
	if ok && now.Sub(lastSeen) <= window {
		d.entries[key] = now
		return true
	}
	d.entries[key] = now
	return false
}

type rawBundle struct {
	Mode          string          `json:"mode"`
	PolicyID      string          `json:"policy_id"`
	PolicyVersion string          `json:"policy_version"`
	Version       string          `json:"version"`
	PolicyHash    string          `json:"policy_hash"`
	Defaults      PolicyDefaults  `json:"defaults"`
	Selectors     PolicySelectors `json:"selectors"`
	Rules         []Rule          `json:"rules"`
}

func LoadFromEnv() Bundle {
	raw := strings.TrimSpace(os.Getenv(policyEnvJSON))
	if raw == "" {
		debugLog("LoadFromEnv: SUB_POLICY_JSON is empty, using default bundle")
		return DefaultBundle()
	}

	debugLog("LoadFromEnv: parsing policy JSON (len=%d)", len(raw))

	var parsed rawBundle
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		debugLog("LoadFromEnv: JSON parse error: %v", err)
		return DefaultBundle()
	}

	version := defaultString(parsed.PolicyVersion, parsed.Version)
	version = defaultString(version, "0.1.0")
	policyID := defaultString(parsed.PolicyID, "default")
	policyHash := strings.TrimSpace(parsed.PolicyHash)
	if policyHash == "" {
		snapshot := buildSnapshot(policyID, version, normalizeModeString(parsed.Mode), parsed.Defaults, parsed.Selectors, parsed.Rules)
		if hash, _, err := hashSnapshot(snapshot); err == nil {
			policyHash = hash
		} else {
			policyHash = "none"
		}
	}

	bundle := Bundle{
		Mode: parseMode(parsed.Mode),
		Info: event.PolicyInfo{
			PolicyID:      policyID,
			PolicyVersion: version,
			PolicyHash:    defaultString(policyHash, "none"),
		},
		Defaults:     parsed.Defaults,
		Selectors:    parsed.Selectors,
		Rules:        parsed.Rules,
		breakerState: make(map[string][]time.Time),
		budgets:      newBudgetState(),
		rateLimit:    newRateLimitState(),
		dedupe:       newDedupeCache(),
	}

	if bundle.Mode == "" {
		bundle.Mode = event.RunModeObserve
	}

	debugLog("LoadFromEnv: mode=%s, rules=%d, budgets=%v, rateLimit=%v, dedupe=%v",
		bundle.Mode, len(bundle.Rules), bundle.budgets != nil, bundle.rateLimit != nil, bundle.dedupe != nil)

	for i, rule := range bundle.Rules {
		debugLog("  Rule[%d]: id=%s, kind=%s, budget=%v, rateLimit=%v, breaker=%v, dedupe=%v",
			i, rule.RuleID, rule.Kind,
			rule.Effect.Budget != nil, rule.Effect.RateLimit != nil,
			rule.Effect.Breaker != nil, rule.Effect.Dedupe != nil)
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
		breakerState: make(map[string][]time.Time),
		budgets:      newBudgetState(),
		rateLimit:    newRateLimitState(),
		dedupe:       newDedupeCache(),
	}
}

func (b *Bundle) Decide(serverName, toolName, argsHash string) Decision {
	return b.DecideWithContext(DecisionContext{
		ServerName: serverName,
		ToolName:   toolName,
		ArgsHash:   argsHash,
	})
}

func (b *Bundle) DecideWithContext(ctx DecisionContext) Decision {
	b.ensureState()
	now := time.Now()
	riskClasses := make(map[string]struct{})

	debugLog("Decide: server=%s, tool=%s, hash=%s", ctx.ServerName, ctx.ToolName, ctx.ArgsHash)

	if !selectorsMatch(b.Selectors, ctx.Target) {
		return Decision{
			Action:     event.DecisionAllow,
			RuleID:     nil,
			ReasonCode: "POLICY_NOT_APPLICABLE",
			Summary:    "Policy selectors did not match",
			Severity:   event.SeverityInfo,
		}
	}

	var orderedDecision *Decision
	var breakerDecision *Decision

	for idx, rule := range b.Rules {
		if !ruleEnabled(rule.Enabled) {
			continue
		}

		if !matchName(rule.Match.ServerName, ctx.ServerName) {
			continue
		}
		if !matchName(rule.Match.ToolName, ctx.ToolName) {
			continue
		}
		if !matchRiskClass(rule.Match.RiskClass, riskClasses) {
			continue
		}
		if !matchArgs(rule.Match.Args, ctx.Args) {
			continue
		}

		debugLog("  Matched Rule[%d] id=%s kind=%s", idx, rule.RuleID, rule.Kind)

		kind := strings.ToLower(strings.TrimSpace(rule.Kind))

		// Check breaker rules (POL-005)
		if kind == "breaker" {
			breaker := rule.Effect.Breaker
			if breaker == nil {
				continue
			}
			if breaker.RepeatThreshold <= 0 || breaker.RepeatWindowMS <= 0 {
				continue
			}

			key := breakerKey(breakerRuleKey(rule, idx), breaker.Scope, ctx.ServerName, ctx.ToolName, ctx.ArgsHash)
			count := b.recordRepeat(key, now, time.Duration(breaker.RepeatWindowMS)*time.Millisecond)
			debugLog("    Breaker: key=%s, count=%d, threshold=%d", key, count, breaker.RepeatThreshold)

			if count >= breaker.RepeatThreshold && breakerDecision == nil {
				debugLog("    Breaker TRIPPED")
				action := b.controlAction(breakerAction(breaker.OnTrip))
				breakerDecision = buildDecision(rule, action, "BREAKER_TRIPPED", "Breaker tripped")
				attachHint(breakerDecision, breaker.HintText, event.HintKindSafety)
			}
			continue
		}

		// Check budget rules (POL-003)
		if isBudgetRule(rule) {
			budgetDec := b.applyBudgetDecision(rule, idx, ctx.ServerName, ctx.ToolName)
			if budgetDec != nil {
				debugLog("    Budget EXCEEDED: action=%s", budgetDec.Action)
				if orderedDecision == nil {
					orderedDecision = budgetDec
				}
			} else {
				debugLog("    Budget OK")
			}
			continue
		}

		// Check rate limit rules (POL-004)
		if rule.Effect.RateLimit != nil {
			rateLimitDec, limited := b.rateLimitDecision(idx, rule, ctx.ServerName, ctx.ToolName)
			if limited {
				debugLog("    RateLimit LIMITED: action=%s, backoff=%d", rateLimitDec.Action, rateLimitDec.BackoffMS)
				return rateLimitDec
			}
			debugLog("    RateLimit OK")
			continue
		}

		// Check dedupe rules (POL-006)
		if strings.EqualFold(rule.Kind, "dedupe") || rule.Effect.Dedupe != nil {
			dedupeDec, blocked := b.evaluateDedupe(rule, ctx.ServerName, ctx.ToolName, ctx.ArgsHash, now)
			if blocked {
				debugLog("    Dedupe BLOCKED")
				return dedupeDec
			}
			debugLog("    Dedupe OK")
			continue
		}

		// Check tag rules (POL-007)
		if kind == "tag" || rule.Effect.Tag != nil {
			applyTag(rule.Effect.Tag, riskClasses)
			continue
		}

		// Check allow/block rules
		action := rule.Effect.Action
		if action == "" {
			action = actionFromKind(kind)
		}
		action = b.controlAction(action)
		if action != event.DecisionAllow && action != event.DecisionBlock && action != event.DecisionRejectWithHint {
			continue
		}

		if orderedDecision == nil {
			debugLog("    Rule Decision: %s", action)
			orderedDecision = buildDecision(rule, action, defaultReason(action), defaultSummary(action))
			attachHint(orderedDecision, "", event.HintKindOther)
		}
	}

	if breakerDecision != nil {
		debugLog("Decide: Returning Breaker Decision %s", breakerDecision.Action)
		return *breakerDecision
	}
	if orderedDecision != nil {
		debugLog("Decide: Returning Ordered Decision %s", orderedDecision.Action)
		return *orderedDecision
	}

	debugLog("Decide: Default Allow")
	return Decision{
		Action:     event.DecisionAllow,
		RuleID:     nil,
		ReasonCode: "DEFAULT_ALLOW",
		Summary:    "Allowed by default policy",
		Severity:   event.SeverityInfo,
	}
}

func (b *Bundle) ensureState() {
	b.breakerMu.Lock()
	if b.breakerState == nil {
		b.breakerState = make(map[string][]time.Time)
	}
	b.breakerMu.Unlock()
	if b.budgets == nil {
		b.budgets = newBudgetState()
	}
	if b.rateLimit == nil {
		b.rateLimit = newRateLimitState()
	}
	if b.dedupe == nil {
		b.dedupe = newDedupeCache()
	}
}

func (b *Bundle) recordRepeat(key string, now time.Time, window time.Duration) int {
	if window <= 0 {
		return 0
	}

	b.breakerMu.Lock()
	defer b.breakerMu.Unlock()
	if b.breakerState == nil {
		b.breakerState = make(map[string][]time.Time)
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
	case "REJECT_WITH_HINT":
		return event.DecisionRejectWithHint
	default:
		return event.DecisionBlock
	}
}

func (b *Bundle) controlAction(action event.DecisionAction) event.DecisionAction {
	if b.Mode != event.RunModeControl {
		return action
	}
	if action == event.DecisionBlock {
		return event.DecisionRejectWithHint
	}
	return action
}

func buildDecision(rule Rule, action event.DecisionAction, fallbackReason, fallbackSummary string) *Decision {
	severity := normalizeSeverity(rule.Severity)
	reason := defaultString(rule.Effect.ReasonCode, fallbackReason)
	summary := defaultString(rule.Effect.Message, fallbackSummary)

	decision := &Decision{
		Action:     action,
		RuleID:     nil,
		ReasonCode: reason,
		Summary:    summary,
		Severity:   severity,
	}
	if action == event.DecisionRejectWithHint {
		decision.Hint = copyHint(rule.Effect.Hint)
	}

	if strings.TrimSpace(rule.RuleID) == "" {
		return decision
	}

	ruleID := rule.RuleID
	decision.RuleID = &ruleID
	return decision
}

func copyHint(hint *event.Hint) *event.Hint {
	if hint == nil {
		return nil
	}

	copied := *hint
	if hint.SuggestedArgs != nil {
		copied.SuggestedArgs = make(map[string]any, len(hint.SuggestedArgs))
		for key, value := range hint.SuggestedArgs {
			copied.SuggestedArgs[key] = value
		}
	}
	if hint.RetryAdvice != nil {
		retryAdvice := *hint.RetryAdvice
		copied.RetryAdvice = &retryAdvice
	}
	return &copied
}

func attachHint(decision *Decision, hintText string, kind event.HintKind) {
	if decision == nil || decision.Action != event.DecisionRejectWithHint {
		return
	}
	if kind == "" {
		kind = event.HintKindOther
	}

	text := strings.TrimSpace(hintText)
	if text == "" {
		text = defaultString(decision.Summary, "Rejected with hint")
	}

	if decision.Hint == nil {
		decision.Hint = &event.Hint{
			HintText: text,
			HintKind: kind,
		}
		return
	}

	if strings.TrimSpace(decision.Hint.HintText) == "" {
		decision.Hint.HintText = text
	}
	if decision.Hint.HintKind == "" {
		decision.Hint.HintKind = kind
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
		debugLog("    applyBudgetDecision: budgets state is NIL! recreating")
		bs = newBudgetState()
		// Fix: assign back to bundle to persist state
		b.budgets = bs
	}

	scope := strings.TrimSpace(strings.ToLower(rule.Effect.Budget.Scope))
	key := budgetKey(rule.RuleID, ruleIndex, scope, serverName, toolName)
	count := bs.incrementCalls(key, 1)

	debugLog("    applyBudgetDecision: key=%s, count=%d, limit=%d", key, count, *limit)

	if count <= *limit {
		return nil
	}

	action := rule.Effect.Budget.OnExceed
	if action == "" {
		action = event.DecisionBlock
	}
	action = b.controlAction(action)
	reason := defaultString(rule.Effect.ReasonCode, "BUDGET_EXCEEDED")
	summary := defaultString(rule.Effect.Message, "Budget exceeded")

	decision := buildDecision(rule, action, reason, summary)
	attachHint(decision, rule.Effect.Budget.HintText, event.HintKindBudget)
	return decision
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
	if b.rateLimit == nil {
		debugLog("    rateLimitDecision: rateLimit state is NIL! recreating")
		b.rateLimit = newRateLimitState()
	}

	config := normalizeRateLimit(rule.Effect.RateLimit)
	if b.rateLimit.allow(ruleIndex, config, serverName, toolName) {
		return Decision{}, false
	}

	action := b.controlAction(config.OnLimit)
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
	if action == event.DecisionRejectWithHint {
		decision.Hint = copyHint(rule.Effect.Hint)
	}
	if action == event.DecisionThrottle {
		decision.BackoffMS = config.BackoffMS
	}
	attachHint(&decision, rule.Effect.RateLimit.HintText, event.HintKindRate)

	if rule.RuleID == "" {
		return decision, true
	}

	ruleID := rule.RuleID
	decision.RuleID = &ruleID
	return decision, true
}

func (b *Bundle) evaluateDedupe(rule Rule, serverName, toolName, argsHash string, now time.Time) (Decision, bool) {
	effect := rule.Effect.Dedupe
	if effect == nil {
		return Decision{}, false
	}
	if !dedupeKeySupported(effect.Key) {
		return Decision{}, false
	}
	if effect.WindowMS <= 0 {
		return Decision{}, false
	}

	dedupeKey := buildDedupeKey(effect.Scope, serverName, toolName, argsHash)
	if dedupeKey == "" {
		return Decision{}, false
	}

	cacheKey := rule.RuleID
	if strings.TrimSpace(cacheKey) == "" {
		cacheKey = "dedupe"
	}
	cacheKey = cacheKey + "|" + dedupeKey

	if b.dedupe == nil {
		debugLog("    evaluateDedupe: dedupe state is NIL! recreating")
		b.dedupe = newDedupeCache()
	}

	window := time.Duration(effect.WindowMS) * time.Millisecond
	if !b.dedupe.IsDuplicate(cacheKey, window, now) {
		return Decision{}, false
	}

	debugLog("    evaluateDedupe: BLOCKED key=%s", cacheKey)

	action := normalizeOnDuplicateAction(effect.OnDuplicate)
	if action == "" {
		action = event.DecisionBlock
	}
	action = b.controlAction(action)

	severity := normalizeSeverity(rule.Severity)
	reason := defaultString(rule.Effect.ReasonCode, "DEDUPE_DUPLICATE")
	summary := defaultString(rule.Effect.Message, "Duplicate call blocked by dedupe window")
	decision := Decision{
		Action:     action,
		RuleID:     nil,
		ReasonCode: reason,
		Summary:    summary,
		Severity:   severity,
	}
	if action == event.DecisionRejectWithHint {
		decision.Hint = copyHint(rule.Effect.Hint)
	}
	if rule.RuleID != "" {
		ruleID := rule.RuleID
		decision.RuleID = &ruleID
	}
	attachHint(&decision, effect.HintText, event.HintKindSafety)
	return decision, true
}

func dedupeKeySupported(key string) bool {
	if strings.TrimSpace(key) == "" {
		return true
	}
	return strings.EqualFold(key, "args_hash")
}

func buildDedupeKey(scope, serverName, toolName, argsHash string) string {
	if strings.TrimSpace(argsHash) == "" {
		return ""
	}

	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", "tool":
		return toolName + "|" + argsHash
	case "server_tool":
		return serverName + "|" + toolName + "|" + argsHash
	case "run":
		return argsHash
	default:
		return ""
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

func matchRiskClass(required []string, classes map[string]struct{}) bool {
	if len(required) == 0 {
		return true
	}
	if len(classes) == 0 {
		return false
	}

	for _, value := range required {
		normalized := normalizeRiskClass(value)
		if normalized == "" {
			continue
		}
		if _, ok := classes[normalized]; ok {
			return true
		}
	}

	return false
}

func applyTag(effect *TagEffect, classes map[string]struct{}) {
	if effect == nil {
		return
	}
	for _, value := range effect.AddRiskClass {
		normalized := normalizeRiskClass(value)
		if normalized == "" {
			continue
		}
		classes[normalized] = struct{}{}
	}
}

func normalizeRiskClass(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
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

func normalizeOnDuplicateAction(action event.DecisionAction) event.DecisionAction {
	switch strings.ToUpper(string(action)) {
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
