package core

import "testing"

func TestReadIdentityFromEnv_PrincipalWorkload(t *testing.T) {
	t.Setenv("SUB_RUN_ID", "run-123")
	t.Setenv("SUB_AGENT_ID", "agent-456")
	t.Setenv("SUB_ENV", "ci")
	t.Setenv("SUB_CLIENT", "codex")
	t.Setenv("SUB_PRINCIPAL", "user@example.com")
	t.Setenv("SUB_WORKLOAD", `{"repo":"subluminal","labels":{"team":"core"}}`)

	id := ReadIdentityFromEnv()

	if id.Principal != "user@example.com" {
		t.Fatalf("expected principal %q, got %q", "user@example.com", id.Principal)
	}

	if id.Workload == nil {
		t.Fatal("expected workload to be set")
	}

	repo, ok := id.Workload["repo"].(string)
	if !ok || repo != "subluminal" {
		t.Fatalf("expected workload.repo %q, got %#v", "subluminal", id.Workload["repo"])
	}

	labels, ok := id.Workload["labels"].(map[string]any)
	if !ok {
		t.Fatalf("expected workload.labels to be an object, got %#v", id.Workload["labels"])
	}

	team, ok := labels["team"].(string)
	if !ok || team != "core" {
		t.Fatalf("expected workload.labels.team %q, got %#v", "core", labels["team"])
	}
}
