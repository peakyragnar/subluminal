package policy

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/subluminal/subluminal/pkg/event"
)

func TestBundleReloadResetsState(t *testing.T) {
	base := Bundle{
		Mode: event.RunModeGuardrails,
		Rules: []Rule{
			{
				RuleID: "budget-rule",
				Kind:   "budget",
				Match: Match{
					ToolName: &NameMatch{Glob: []string{"tool"}},
				},
				Effect: Effect{
					Budget: &BudgetEffect{
						Scope:      "tool",
						LimitCalls: intPtr(1),
						OnExceed:   event.DecisionBlock,
					},
				},
			},
		},
		breakerState: make(map[string][]time.Time),
		budgets:      newBudgetState(),
		rateLimit:    newRateLimitState(),
		dedupe:       newDedupeCache(),
	}

	_ = base.Decide("server", "tool", "hash")
	if len(base.budgets.calls) == 0 {
		t.Fatal("expected budget state to record calls before reload")
	}

	next := Bundle{
		Mode:         base.Mode,
		Rules:        base.Rules,
		breakerState: base.breakerState,
		budgets:      base.budgets,
		rateLimit:    base.rateLimit,
		dedupe:       base.dedupe,
	}

	reloaded := base.Reload(next)
	if reloaded.budgets == base.budgets {
		t.Fatal("expected budgets state to be reset on reload")
	}
	if len(reloaded.budgets.calls) != 0 {
		t.Fatalf("expected budgets state to be empty after reload, got %d", len(reloaded.budgets.calls))
	}
	if reloaded.rateLimit == base.rateLimit {
		t.Fatal("expected rate limit state to be reset on reload")
	}
	if reloaded.dedupe == base.dedupe {
		t.Fatal("expected dedupe state to be reset on reload")
	}
	if len(reloaded.breakerState) != 0 {
		t.Fatalf("expected breaker state to be reset on reload, got %d", len(reloaded.breakerState))
	}
}

func TestBundleWatcherDetectsChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")

	if err := os.WriteFile(path, []byte(validPolicyYAML("1.0.0")), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	watcher := NewBundleWatcher(path)
	compiled, changed, err := watcher.Check()
	if err != nil {
		t.Fatalf("initial check error: %v", err)
	}
	if !changed {
		t.Fatal("expected initial change to be detected")
	}
	if compiled.Bundle.Info.PolicyVersion != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %q", compiled.Bundle.Info.PolicyVersion)
	}

	_, changed, err = watcher.Check()
	if err != nil {
		t.Fatalf("second check error: %v", err)
	}
	if changed {
		t.Fatal("expected no change without file update")
	}

	if err := os.WriteFile(path, []byte(validPolicyYAML("1.0.1")), 0o600); err != nil {
		t.Fatalf("write updated policy: %v", err)
	}

	compiled, changed, err = watcher.Check()
	if err != nil {
		t.Fatalf("updated check error: %v", err)
	}
	if !changed {
		t.Fatal("expected change after file update")
	}
	if compiled.Bundle.Info.PolicyVersion != "1.0.1" {
		t.Fatalf("expected version 1.0.1, got %q", compiled.Bundle.Info.PolicyVersion)
	}
}

func TestBundleWatcherRejectsInvalidPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")

	if err := os.WriteFile(path, []byte(validPolicyYAML("1.0.0")), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	watcher := NewBundleWatcher(path)
	if _, changed, err := watcher.Check(); err != nil || !changed {
		t.Fatalf("expected initial policy load, changed=%v err=%v", changed, err)
	}

	invalid := `policy_id: invalid
policy_version: "1.0.0"
mode: guardrails
rules: []
`
	if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
		t.Fatalf("write invalid policy: %v", err)
	}

	if _, changed, err := watcher.Check(); err == nil || !changed {
		t.Fatalf("expected validation error on change, changed=%v err=%v", changed, err)
	}

	if err := os.WriteFile(path, []byte(validPolicyYAML("1.0.2")), 0o600); err != nil {
		t.Fatalf("write valid policy: %v", err)
	}

	compiled, changed, err := watcher.Check()
	if err != nil {
		t.Fatalf("expected recovery after fix, got err=%v", err)
	}
	if !changed {
		t.Fatal("expected change after fixing policy")
	}
	if compiled.Bundle.Info.PolicyVersion != "1.0.2" {
		t.Fatalf("expected version 1.0.2, got %q", compiled.Bundle.Info.PolicyVersion)
	}
}

func validPolicyYAML(version string) string {
	return `policy_id: test-policy
policy_version: "` + version + `"
mode: guardrails
rules:
  - rule_id: allow-all
    kind: allow
    match:
      server_name:
        glob: ["*"]
      tool_name:
        glob: ["*"]
    effect:
      action: ALLOW
      reason_code: OK
      message: ok
`
}
