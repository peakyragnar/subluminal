package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/subluminal/subluminal/pkg/canonical"
	"github.com/subluminal/subluminal/pkg/event"
	"github.com/subluminal/subluminal/pkg/policy"
)

func runPolicy(args []string) int {
	if len(args) == 0 {
		policyUsage()
		return 2
	}

	switch args[0] {
	case "lint":
		return runPolicyLint(args[1:])
	case "diff":
		return runPolicyDiff(args[1:])
	case "explain":
		return runPolicyExplain(args[1:])
	case "-h", "--help", "help":
		policyUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown policy command: %s\n", args[0])
		policyUsage()
		return 2
	}
}

func policyUsage() {
	fmt.Fprintln(os.Stderr, "Usage: sub policy <lint|diff|explain> [options]")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  lint <bundle>")
	fmt.Fprintln(os.Stderr, "  diff <old> <new>")
	fmt.Fprintln(os.Stderr, "  explain <bundle> --server NAME --tool NAME [--args JSON]")
}

func runPolicyLint(args []string) int {
	flags := flag.NewFlagSet("policy lint", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	jsonOnly := flags.Bool("json", false, "Output JSON only")

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: sub policy lint <bundle>")
		return 2
	}

	spec, err := policy.LoadBundleFile(flags.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "lint error: %v\n", err)
		return 1
	}

	issues := policy.LintBundle(spec)
	if *jsonOnly {
		return emitJSON(issues)
	}

	hasError := false
	if len(issues) == 0 {
		fmt.Fprintln(os.Stderr, "OK: no issues found")
		return 0
	}
	for _, issue := range issues {
		if issue.Level == "error" {
			hasError = true
		}
		fmt.Fprintf(os.Stderr, "%s: %s (%s)\n", issue.Level, issue.Message, issue.Field)
	}
	if hasError {
		return 1
	}
	return 0
}

func runPolicyDiff(args []string) int {
	flags := flag.NewFlagSet("policy diff", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	jsonOnly := flags.Bool("json", false, "Output JSON only")

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "Usage: sub policy diff <old> <new>")
		return 2
	}

	oldSpec, err := policy.LoadBundleFile(flags.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "diff error: %v\n", err)
		return 1
	}
	newSpec, err := policy.LoadBundleFile(flags.Arg(1))
	if err != nil {
		fmt.Fprintf(os.Stderr, "diff error: %v\n", err)
		return 1
	}

	result := policy.DiffBundles(oldSpec, newSpec)
	hasChanges := len(result.Changes) > 0
	if *jsonOnly {
		if emitJSON(result) != 0 {
			return 1
		}
		if hasChanges {
			return 1
		}
		return 0
	}

	if !hasChanges {
		fmt.Fprintln(os.Stderr, "No policy changes.")
		return 0
	}

	added, removed, changed, metadata := splitPolicyDiffChanges(result.Changes)
	printedSection := false
	if len(added) > 0 {
		printedSection = printPolicyDiffSection("Rules added:", printedSection)
		for _, change := range added {
			rule, ok := change.After.(policy.Rule)
			if !ok {
				fmt.Fprintf(os.Stderr, "  + %s\n", change.RuleID)
				continue
			}
			fmt.Fprintf(os.Stderr, "  + %s\n", ruleLabel(rule))
			fmt.Fprint(os.Stderr, formatRuleDetails(rule, "    "))
		}
	}
	if len(removed) > 0 {
		printedSection = printPolicyDiffSection("Rules removed:", printedSection)
		for _, change := range removed {
			rule, ok := change.Before.(policy.Rule)
			if !ok {
				fmt.Fprintf(os.Stderr, "  - %s\n", change.RuleID)
				continue
			}
			fmt.Fprintf(os.Stderr, "  - %s\n", ruleLabel(rule))
			fmt.Fprint(os.Stderr, formatRuleDetails(rule, "    "))
		}
	}
	if len(changed) > 0 {
		printedSection = printPolicyDiffSection("Rules changed:", printedSection)
		for _, change := range changed {
			rule, ok := change.After.(policy.Rule)
			if !ok {
				rule, ok = change.Before.(policy.Rule)
			}
			if ok {
				fmt.Fprintf(os.Stderr, "  ~ %s\n", ruleLabel(rule))
			} else if change.RuleID != "" {
				fmt.Fprintf(os.Stderr, "  ~ %s\n", change.RuleID)
			} else {
				fmt.Fprintln(os.Stderr, "  ~ rule modified")
			}
			if len(change.Fields) == 0 {
				fmt.Fprintln(os.Stderr, "    - (details unavailable)")
				continue
			}
			for _, field := range change.Fields {
				fmt.Fprintf(os.Stderr, "    - %s: %s -> %s\n", field.Field, formatDiffValue(field.Before), formatDiffValue(field.After))
			}
		}
	}
	if len(metadata) > 0 {
		printedSection = printPolicyDiffSection("Policy metadata changes:", printedSection)
		for _, change := range metadata {
			fmt.Fprintf(os.Stderr, "  - %s\n", formatMetadataChange(change))
		}
	}

	return 1
}

func runPolicyExplain(args []string) int {
	flags := flag.NewFlagSet("policy explain", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	serverName := flags.String("server", "", "Server name")
	toolName := flags.String("tool", "", "Tool name")
	argsJSON := flags.String("args", "", "Tool args JSON")
	env := flags.String("env", "", "Env selector value")
	agentID := flags.String("agent-id", "", "Agent ID selector value")
	client := flags.String("client", "", "Client selector value")
	workloadJSON := flags.String("workload", "", "Workload JSON")
	jsonOnly := flags.Bool("json", false, "Output JSON only")

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 || *serverName == "" || *toolName == "" {
		fmt.Fprintln(os.Stderr, "Usage: sub policy explain <bundle> --server NAME --tool NAME [--args JSON]")
		return 2
	}

	spec, err := policy.LoadBundleFile(flags.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "explain error: %v\n", err)
		return 1
	}

	compiled, err := policy.CompileBundle(spec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "explain error: %v\n", err)
		return 1
	}

	var argsPayload map[string]any
	if strings.TrimSpace(*argsJSON) != "" {
		if err := json.Unmarshal([]byte(*argsJSON), &argsPayload); err != nil {
			fmt.Fprintf(os.Stderr, "explain error: invalid args JSON: %v\n", err)
			return 1
		}
	}

	var workload policy.WorkloadContext
	if strings.TrimSpace(*workloadJSON) != "" {
		if err := json.Unmarshal([]byte(*workloadJSON), &workload); err != nil {
			fmt.Fprintf(os.Stderr, "explain error: invalid workload JSON: %v\n", err)
			return 1
		}
	}

	argsHash := ""
	if argsPayload != nil {
		if hash, err := canonical.ArgsHash(argsPayload); err == nil {
			argsHash = hash
		}
	}

	target := policy.SelectorTarget{
		Env:      *env,
		AgentID:  *agentID,
		Client:   *client,
		Workload: workload,
	}

	decision := compiled.Bundle.DecideWithContext(policy.DecisionContext{
		ServerName: *serverName,
		ToolName:   *toolName,
		ArgsHash:   argsHash,
		Args:       argsPayload,
		Target:     target,
	})

	output := explainOutput{
		Input: explainInput{
			ServerName: *serverName,
			ToolName:   *toolName,
			ArgsHash:   argsHash,
		},
		Decision: decisionOutput{
			Action:     decision.Action,
			RuleID:     decision.RuleID,
			ReasonCode: decision.ReasonCode,
			Summary:    decision.Summary,
			Severity:   decision.Severity,
			BackoffMS:  decision.BackoffMS,
			Hint:       decision.Hint,
		},
		Policy: compiled.Bundle.Info,
	}

	if !*jsonOnly {
		fmt.Fprintf(os.Stderr, "Decision: %s\n", decision.Action)
		if decision.RuleID != nil {
			fmt.Fprintf(os.Stderr, "Rule: %s\n", *decision.RuleID)
		}
		fmt.Fprintf(os.Stderr, "Reason: %s\n", decision.ReasonCode)
	}

	return emitJSON(output)
}

type explainInput struct {
	ServerName string `json:"server_name"`
	ToolName   string `json:"tool_name"`
	ArgsHash   string `json:"args_hash,omitempty"`
}

type decisionOutput struct {
	Action     event.DecisionAction `json:"action"`
	RuleID     *string              `json:"rule_id,omitempty"`
	ReasonCode string               `json:"reason_code"`
	Summary    string               `json:"summary"`
	Severity   event.Severity       `json:"severity"`
	BackoffMS  int                  `json:"backoff_ms,omitempty"`
	Hint       *event.Hint          `json:"hint,omitempty"`
}

type explainOutput struct {
	Input    explainInput     `json:"input"`
	Decision decisionOutput   `json:"decision"`
	Policy   event.PolicyInfo `json:"policy"`
}

func emitJSON(value any) int {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "json error: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stdout, string(payload))
	return 0
}

func splitPolicyDiffChanges(changes []policy.DiffChange) (added, removed, modified, metadata []policy.DiffChange) {
	for _, change := range changes {
		switch change.Kind {
		case "rule_added":
			added = append(added, change)
		case "rule_removed":
			removed = append(removed, change)
		case "rule_modified":
			modified = append(modified, change)
		default:
			metadata = append(metadata, change)
		}
	}
	return added, removed, modified, metadata
}

func printPolicyDiffSection(title string, printed bool) bool {
	if printed {
		fmt.Fprintln(os.Stderr)
	}
	fmt.Fprintln(os.Stderr, title)
	return true
}

func ruleLabel(rule policy.Rule) string {
	kind := strings.ToLower(strings.TrimSpace(rule.Kind))
	if kind == "" {
		kind = "rule"
	}
	ruleID := strings.TrimSpace(rule.RuleID)
	if ruleID == "" {
		return kind
	}
	return fmt.Sprintf("%s:%s", kind, ruleID)
}

func formatRuleDetails(rule policy.Rule, indent string) string {
	payload, err := json.MarshalIndent(rule, "", "  ")
	if err != nil {
		return fmt.Sprintf("%serror formatting rule: %v\n", indent, err)
	}
	lines := strings.Split(string(payload), "\n")
	for i, line := range lines {
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n") + "\n"
}

func formatDiffValue(value any) string {
	if value == nil {
		return "null"
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(payload)
}

func formatMetadataChange(change policy.DiffChange) string {
	if change.Field == "" {
		return change.Summary
	}
	return fmt.Sprintf("%s: %s -> %s", change.Field, formatDiffValue(change.Before), formatDiffValue(change.After))
}
