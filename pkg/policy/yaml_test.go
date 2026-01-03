package policy

import "testing"

func TestParseYAMLBundleInlineListKeys(t *testing.T) {
	yaml := `
mode: guardrails
policy_id: test-policy
rules:
  - rule_id: allow-all
    kind: allow
    effect:
      action: ALLOW
      reason_code: OK
      message: ok
`

	raw, err := parseYAMLBundle(yaml)
	if err != nil {
		t.Fatalf("parseYAMLBundle error: %v", err)
	}

	spec, err := decodeBundle(raw)
	if err != nil {
		t.Fatalf("decodeBundle error: %v", err)
	}
	if len(spec.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(spec.Rules))
	}
	if spec.Rules[0].RuleID != "allow-all" {
		t.Fatalf("expected rule_id allow-all, got %q", spec.Rules[0].RuleID)
	}
}
