package policy

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/peakyragnar/subluminal/pkg/event"
)

// LintIssue represents a validation finding for policy bundles.
type LintIssue struct {
	Level   string `json:"level"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// LintBundle validates a bundle spec and returns any issues.
func LintBundle(spec BundleSpec) []LintIssue {
	var issues []LintIssue

	if strings.TrimSpace(spec.PolicyID) == "" {
		issues = append(issues, LintIssue{Level: "error", Field: "policy_id", Message: "policy_id is required"})
	}
	if strings.TrimSpace(spec.EffectiveVersion()) == "" {
		issues = append(issues, LintIssue{Level: "error", Field: "version", Message: "version is required"})
	}

	mode := strings.ToLower(strings.TrimSpace(spec.Mode))
	switch mode {
	case "":
		issues = append(issues, LintIssue{Level: "warn", Field: "mode", Message: "mode missing; defaults to observe"})
	case "observe", "guardrails", "control":
	default:
		issues = append(issues, LintIssue{Level: "error", Field: "mode", Message: "mode must be observe|guardrails|control"})
	}

	if spec.Defaults.DecisionOnError != "" {
		switch strings.ToUpper(strings.TrimSpace(spec.Defaults.DecisionOnError)) {
		case "ALLOW", "BLOCK":
		default:
			issues = append(issues, LintIssue{Level: "error", Field: "defaults.decision_on_error", Message: "decision_on_error must be ALLOW or BLOCK"})
		}
	}

	if len(spec.Rules) == 0 {
		issues = append(issues, LintIssue{Level: "error", Field: "rules", Message: "at least one rule is required"})
	}

	seenRuleIDs := map[string]struct{}{}
	for i, rule := range spec.Rules {
		ruleField := fmt.Sprintf("rules[%d]", i)
		if strings.TrimSpace(rule.RuleID) == "" {
			issues = append(issues, LintIssue{Level: "error", Field: ruleField + ".rule_id", Message: "rule_id is required"})
		} else if _, exists := seenRuleIDs[rule.RuleID]; exists {
			issues = append(issues, LintIssue{Level: "error", Field: ruleField + ".rule_id", Message: "rule_id must be unique"})
		} else {
			seenRuleIDs[rule.RuleID] = struct{}{}
		}

		kind := strings.ToLower(strings.TrimSpace(rule.Kind))
		switch kind {
		case "allow", "deny", "budget", "rate_limit", "breaker", "dedupe", "tag":
		case "":
			issues = append(issues, LintIssue{Level: "error", Field: ruleField + ".kind", Message: "kind is required"})
		default:
			issues = append(issues, LintIssue{Level: "error", Field: ruleField + ".kind", Message: "unsupported rule kind"})
		}

		if rule.Severity != "" {
			switch strings.ToLower(string(rule.Severity)) {
			case "info", "warn", "critical":
			default:
				issues = append(issues, LintIssue{Level: "warn", Field: ruleField + ".severity", Message: "severity should be info|warn|critical"})
			}
		}

		for _, pattern := range rule.Match.ServerName.Regex {
			if _, err := regexp.Compile(pattern); err != nil {
				issues = append(issues, LintIssue{Level: "error", Field: ruleField + ".match.server_name.regex", Message: err.Error()})
			}
		}
		for _, pattern := range rule.Match.ToolName.Regex {
			if _, err := regexp.Compile(pattern); err != nil {
				issues = append(issues, LintIssue{Level: "error", Field: ruleField + ".match.tool_name.regex", Message: err.Error()})
			}
		}

		if rule.Match.Args != nil {
			for key, numeric := range rule.Match.Args.NumericRange {
				if numeric.Min != nil && numeric.Max != nil && *numeric.Min > *numeric.Max {
					issues = append(issues, LintIssue{Level: "error", Field: ruleField + ".match.args.numeric_range." + key, Message: "min cannot exceed max"})
				}
			}
			for key, values := range rule.Match.Args.KeyIn {
				if len(values) == 0 {
					issues = append(issues, LintIssue{Level: "warn", Field: ruleField + ".match.args.key_in." + key, Message: "key_in list is empty"})
				}
			}
		}
	}

	if spec.PolicyHash != "" {
		if compiled, err := CompileBundle(spec); err == nil {
			if compiled.Hash != spec.PolicyHash {
				issues = append(issues, LintIssue{Level: "warn", Field: "policy_hash", Message: "policy_hash does not match computed snapshot hash"})
			}
		}
	}

	return issues
}

// DiffChange describes a policy change.
type DiffChange struct {
	Kind     string `json:"kind"`
	RuleID   string `json:"rule_id,omitempty"`
	Field    string `json:"field,omitempty"`
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
	Before   any    `json:"before,omitempty"`
	After    any    `json:"after,omitempty"`
}

// DiffResult aggregates policy changes.
type DiffResult struct {
	Severity string       `json:"severity"`
	Summary  string       `json:"summary"`
	Changes  []DiffChange `json:"changes"`
}

// DiffBundles compares two bundle specs and returns a change summary.
func DiffBundles(oldSpec, newSpec BundleSpec) DiffResult {
	var changes []DiffChange

	if strings.TrimSpace(oldSpec.PolicyID) != strings.TrimSpace(newSpec.PolicyID) {
		changes = append(changes, DiffChange{
			Kind:     "policy_id",
			Field:    "policy_id",
			Severity: "warn",
			Summary:  "policy_id changed",
			Before:   oldSpec.PolicyID,
			After:    newSpec.PolicyID,
		})
	}

	if oldSpec.EffectiveVersion() != newSpec.EffectiveVersion() {
		changes = append(changes, DiffChange{
			Kind:     "version",
			Field:    "version",
			Severity: "info",
			Summary:  "version changed",
			Before:   oldSpec.EffectiveVersion(),
			After:    newSpec.EffectiveVersion(),
		})
	}

	oldMode := normalizeModeString(oldSpec.Mode)
	newMode := normalizeModeString(newSpec.Mode)
	if oldMode != newMode {
		changes = append(changes, DiffChange{
			Kind:     "mode",
			Field:    "mode",
			Severity: modeChangeSeverity(oldMode, newMode),
			Summary:  fmt.Sprintf("mode changed from %s to %s", oldMode, newMode),
			Before:   oldMode,
			After:    newMode,
		})
	}

	if !reflect.DeepEqual(oldSpec.Defaults, newSpec.Defaults) {
		changes = append(changes, DiffChange{
			Kind:     "defaults",
			Field:    "defaults",
			Severity: "info",
			Summary:  "defaults changed",
			Before:   oldSpec.Defaults,
			After:    newSpec.Defaults,
		})
	}

	if !reflect.DeepEqual(oldSpec.Selectors, newSpec.Selectors) {
		changes = append(changes, DiffChange{
			Kind:     "selectors",
			Field:    "selectors",
			Severity: "info",
			Summary:  "selectors changed",
			Before:   oldSpec.Selectors,
			After:    newSpec.Selectors,
		})
	}

	oldRules := rulesByID(oldSpec.Rules)
	newRules := rulesByID(newSpec.Rules)

	for ruleID, oldRule := range oldRules {
		if _, ok := newRules[ruleID]; !ok {
			changes = append(changes, DiffChange{
				Kind:     "rule_removed",
				RuleID:   ruleID,
				Severity: ruleRemovalSeverity(oldRule),
				Summary:  "rule removed",
				Before:   oldRule,
			})
		}
	}

	for ruleID, newRule := range newRules {
		if _, ok := oldRules[ruleID]; !ok {
			changes = append(changes, DiffChange{
				Kind:     "rule_added",
				RuleID:   ruleID,
				Severity: ruleAdditionSeverity(newRule),
				Summary:  "rule added",
				After:    newRule,
			})
		}
	}

	for ruleID, oldRule := range oldRules {
		newRule, ok := newRules[ruleID]
		if !ok {
			continue
		}
		if reflect.DeepEqual(oldRule, newRule) {
			continue
		}
		changes = append(changes, DiffChange{
			Kind:     "rule_modified",
			RuleID:   ruleID,
			Severity: ruleModificationSeverity(oldRule, newRule),
			Summary:  "rule modified",
			Before:   oldRule,
			After:    newRule,
		})
	}

	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Kind != changes[j].Kind {
			return changes[i].Kind < changes[j].Kind
		}
		if changes[i].RuleID != changes[j].RuleID {
			return changes[i].RuleID < changes[j].RuleID
		}
		return changes[i].Summary < changes[j].Summary
	})

	severity := "info"
	for _, change := range changes {
		severity = maxSeverity(severity, change.Severity)
	}

	summary := fmt.Sprintf("%d change(s)", len(changes))
	return DiffResult{
		Severity: severity,
		Summary:  summary,
		Changes:  changes,
	}
}

func rulesByID(rules []Rule) map[string]Rule {
	out := map[string]Rule{}
	for _, rule := range rules {
		if strings.TrimSpace(rule.RuleID) == "" {
			continue
		}
		if !ruleEnabled(rule.Enabled) {
			continue
		}
		out[rule.RuleID] = rule
	}
	return out
}

func modeChangeSeverity(oldMode, newMode string) string {
	if newMode == "observe" && oldMode != "observe" {
		return "critical"
	}
	if oldMode == "observe" && newMode != "observe" {
		return "info"
	}
	return "warn"
}

func ruleAdditionSeverity(rule Rule) string {
	action := ruleAction(rule)
	if action == event.DecisionAllow {
		return "critical"
	}
	if isBlockish(action) {
		return "warn"
	}
	return "info"
}

func ruleRemovalSeverity(rule Rule) string {
	action := ruleAction(rule)
	if isBlockish(action) {
		return "critical"
	}
	if action == event.DecisionAllow {
		return "info"
	}
	return "warn"
}

func ruleModificationSeverity(oldRule, newRule Rule) string {
	oldAction := ruleAction(oldRule)
	newAction := ruleAction(newRule)
	if isBlockish(oldAction) && newAction == event.DecisionAllow {
		return "critical"
	}
	if oldAction == event.DecisionAllow && isBlockish(newAction) {
		return "warn"
	}
	return "info"
}

func ruleAction(rule Rule) event.DecisionAction {
	if rule.Effect.Action != "" {
		return rule.Effect.Action
	}
	kind := strings.ToLower(strings.TrimSpace(rule.Kind))
	if action := actionFromKind(kind); action != "" {
		return action
	}
	if rule.Effect.Budget != nil && rule.Effect.Budget.OnExceed != "" {
		return rule.Effect.Budget.OnExceed
	}
	if rule.Effect.RateLimit != nil && rule.Effect.RateLimit.OnLimit != "" {
		return rule.Effect.RateLimit.OnLimit
	}
	if rule.Effect.Dedupe != nil && rule.Effect.Dedupe.OnDuplicate != "" {
		return rule.Effect.Dedupe.OnDuplicate
	}
	if rule.Effect.Breaker != nil {
		return breakerAction(rule.Effect.Breaker.OnTrip)
	}
	return ""
}

func isBlockish(action event.DecisionAction) bool {
	switch action {
	case event.DecisionBlock, event.DecisionRejectWithHint, event.DecisionTerminateRun, event.DecisionThrottle:
		return true
	default:
		return false
	}
}

func maxSeverity(current, candidate string) string {
	order := map[string]int{
		"info":     0,
		"warn":     1,
		"critical": 2,
	}
	if order[candidate] > order[current] {
		return candidate
	}
	return current
}
