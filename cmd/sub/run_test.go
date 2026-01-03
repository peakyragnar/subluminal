package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunSetsDefaults(t *testing.T) {
	resetEnv(t)

	args := []string{"--", os.Args[0], "-test.run=TestHelperProcess", "--", "--helper-process"}

	output := captureStdout(t, func() {
		code := runRun(args)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
	})

	env := parseEnvJSON(t, output)
	if env["SUB_RUN_ID"] == "" {
		t.Fatal("expected SUB_RUN_ID to be set")
	}
	if env["SUB_AGENT_ID"] != "unknown" {
		t.Fatalf("expected SUB_AGENT_ID=unknown, got %q", env["SUB_AGENT_ID"])
	}
	if env["SUB_CLIENT"] != "custom" {
		t.Fatalf("expected SUB_CLIENT=custom, got %q", env["SUB_CLIENT"])
	}
	if env["SUB_ENV"] != "dev" {
		t.Fatalf("expected SUB_ENV=dev, got %q", env["SUB_ENV"])
	}
	if env["SUB_PRINCIPAL"] != "" {
		t.Fatalf("expected SUB_PRINCIPAL empty, got %q", env["SUB_PRINCIPAL"])
	}
}

func TestRunHonorsFlags(t *testing.T) {
	resetEnv(t)

	args := []string{
		"--run", "run-123",
		"--agent", "agent-abc",
		"--client", "claude",
		"--env", "ci",
		"--principal", "user-1",
		"--", os.Args[0], "-test.run=TestHelperProcess", "--", "--helper-process",
	}

	output := captureStdout(t, func() {
		code := runRun(args)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
	})

	env := parseEnvJSON(t, output)
	if env["SUB_RUN_ID"] != "run-123" {
		t.Fatalf("expected SUB_RUN_ID=run-123, got %q", env["SUB_RUN_ID"])
	}
	if env["SUB_AGENT_ID"] != "agent-abc" {
		t.Fatalf("expected SUB_AGENT_ID=agent-abc, got %q", env["SUB_AGENT_ID"])
	}
	if env["SUB_CLIENT"] != "claude" {
		t.Fatalf("expected SUB_CLIENT=claude, got %q", env["SUB_CLIENT"])
	}
	if env["SUB_ENV"] != "ci" {
		t.Fatalf("expected SUB_ENV=ci, got %q", env["SUB_ENV"])
	}
	if env["SUB_PRINCIPAL"] != "user-1" {
		t.Fatalf("expected SUB_PRINCIPAL=user-1, got %q", env["SUB_PRINCIPAL"])
	}
}

func TestHelperProcess(t *testing.T) {
	if !hasArg("--helper-process") {
		return
	}

	env := map[string]string{
		"SUB_RUN_ID":    os.Getenv("SUB_RUN_ID"),
		"SUB_AGENT_ID":  os.Getenv("SUB_AGENT_ID"),
		"SUB_CLIENT":    os.Getenv("SUB_CLIENT"),
		"SUB_ENV":       os.Getenv("SUB_ENV"),
		"SUB_PRINCIPAL": os.Getenv("SUB_PRINCIPAL"),
	}

	data, err := json.Marshal(env)
	if err != nil {
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(data)
	os.Exit(0)
}

func resetEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SUB_RUN_ID", "")
	t.Setenv("SUB_AGENT_ID", "")
	t.Setenv("SUB_CLIENT", "")
	t.Setenv("SUB_ENV", "")
	t.Setenv("SUB_PRINCIPAL", "")
}

func parseEnvJSON(t *testing.T, output string) map[string]string {
	t.Helper()

	output = strings.TrimSpace(output)
	if output == "" {
		t.Fatal("expected helper output")
	}

	env := make(map[string]string)
	if err := json.Unmarshal([]byte(output), &env); err != nil {
		t.Fatalf("failed to parse helper JSON: %v", err)
	}
	return env
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer reader.Close()

	oldStdout := os.Stdout
	os.Stdout = writer
	defer func() {
		os.Stdout = oldStdout
	}()

	outputCh := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		outputCh <- buf.String()
	}()

	fn()
	_ = writer.Close()
	return <-outputCh
}

func hasArg(arg string) bool {
	for _, value := range os.Args {
		if value == arg {
			return true
		}
	}
	return false
}
