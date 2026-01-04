package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/subluminal/subluminal/pkg/policy"
)

const (
	basePolicyJSON = `{
  "policy_id": "test-policy",
  "version": "1",
  "mode": "guardrails",
  "rules": [
    {
      "rule_id": "allow-all",
      "kind": "allow",
      "effect": {
        "action": "ALLOW",
        "reason_code": "OK",
        "message": "ok"
      }
    }
  ]
}`

	oldPolicyJSON = `{
  "policy_id": "test-policy",
  "version": "1",
  "mode": "guardrails",
  "rules": [
    {
      "rule_id": "legacy-api",
      "kind": "allow",
      "match": {
        "tool_name": {
          "glob": [
            "old_api_*"
          ]
        }
      },
      "effect": {
        "action": "ALLOW",
        "reason_code": "OK",
        "message": "ok"
      }
    },
    {
      "rule_id": "token-limit",
      "kind": "budget",
      "effect": {
        "budget": {
          "scope": "tool",
          "limit_calls": 1000,
          "on_exceed": "BLOCK"
        }
      }
    }
  ]
}`

	newPolicyJSON = `{
  "policy_id": "test-policy",
  "version": "1",
  "mode": "guardrails",
  "rules": [
    {
      "rule_id": "token-limit",
      "kind": "budget",
      "effect": {
        "budget": {
          "scope": "tool",
          "limit_calls": 2000,
          "on_exceed": "BLOCK"
        }
      }
    },
    {
      "rule_id": "high-risk-tools",
      "kind": "deny",
      "match": {
        "tool_name": {
          "glob": [
            "rm_*"
          ]
        }
      },
      "effect": {
        "action": "BLOCK",
        "reason_code": "NOPE",
        "message": "nope"
      }
    }
  ]
}`
)

func TestPolicyDiffIdentical(t *testing.T) {
	oldPath := writeTempPolicy(t, basePolicyJSON)
	newPath := writeTempPolicy(t, basePolicyJSON)

	_, stderr := captureOutput(t, func() {
		code := runPolicyDiff([]string{oldPath, newPath})
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
	})

	if !strings.Contains(stderr, "No policy changes.") {
		t.Fatalf("expected no-change message, got: %s", stderr)
	}
}

func TestPolicyDiffHumanReadable(t *testing.T) {
	oldPath := writeTempPolicy(t, oldPolicyJSON)
	newPath := writeTempPolicy(t, newPolicyJSON)

	_, stderr := captureOutput(t, func() {
		code := runPolicyDiff([]string{oldPath, newPath})
		if code != 1 {
			t.Fatalf("expected exit code 1, got %d", code)
		}
	})

	want := []string{
		"Rules added:",
		"+ deny:high-risk-tools",
		"\"rule_id\": \"high-risk-tools\"",
		"Rules removed:",
		"- allow:legacy-api",
		"\"rule_id\": \"legacy-api\"",
		"Rules changed:",
		"~ budget:token-limit",
		"effect.budget.limit_calls: 1000 -> 2000",
	}
	for _, snippet := range want {
		if !strings.Contains(stderr, snippet) {
			t.Fatalf("missing output %q in:\n%s", snippet, stderr)
		}
	}
}

func TestPolicyDiffJSON(t *testing.T) {
	oldPath := writeTempPolicy(t, oldPolicyJSON)
	newPath := writeTempPolicy(t, newPolicyJSON)

	stdout, stderr := captureOutput(t, func() {
		code := runPolicyDiff([]string{"--json", oldPath, newPath})
		if code != 1 {
			t.Fatalf("expected exit code 1, got %d", code)
		}
	})

	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected no stderr output, got: %s", stderr)
	}

	var result policy.DiffResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	kinds := make(map[string]bool)
	foundField := false
	for _, change := range result.Changes {
		kinds[change.Kind] = true
		if change.Kind != "rule_modified" {
			continue
		}
		for _, field := range change.Fields {
			if field.Field == "effect.budget.limit_calls" {
				foundField = true
			}
		}
	}

	for _, kind := range []string{"rule_added", "rule_removed", "rule_modified"} {
		if !kinds[kind] {
			t.Fatalf("expected change kind %q in JSON output", kind)
		}
	}
	if !foundField {
		t.Fatal("expected field diff for effect.budget.limit_calls")
	}
}

func writeTempPolicy(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir()
	file, err := os.CreateTemp(path, "policy-*.json")
	if err != nil {
		t.Fatalf("create temp policy: %v", err)
	}
	if _, err := file.WriteString(content); err != nil {
		t.Fatalf("write temp policy: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close temp policy: %v", err)
	}
	return file.Name()
}

func captureOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter

	outputCh := make(chan string, 1)
	errorCh := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, stdoutReader)
		outputCh <- buf.String()
	}()
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, stderrReader)
		errorCh <- buf.String()
	}()

	fn()
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()

	os.Stdout = oldStdout
	os.Stderr = oldStderr

	stdout := <-outputCh
	stderr := <-errorCh
	_ = stdoutReader.Close()
	_ = stderrReader.Close()
	return stdout, stderr
}
