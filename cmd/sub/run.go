package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/subluminal/subluminal/pkg/core"
)

func runRun(args []string) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	runIDFlag := flags.String("run", "", "Run ID (default: generate)")
	agentIDFlag := flags.String("agent", "", "Agent ID (default: unknown)")
	clientFlag := flags.String("client", "", "Client type (claude|codex|headless|custom|unknown)")
	envFlag := flags.String("env", "", "Environment (dev|ci|prod|unknown)")
	principalFlag := flags.String("principal", "", "Principal (optional)")
	printRunID := flags.Bool("print-run-id", false, "Print run_id before executing")

	if err := flags.Parse(args); err != nil {
		return 2
	}

	cmdArgs := flags.Args()
	if len(cmdArgs) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: sub run [options] -- <command> [args...]")
		flags.Usage()
		return 2
	}

	runID := pickValue(*runIDFlag, os.Getenv("SUB_RUN_ID"))
	if runID == "" {
		runID = core.GenerateUUID()
	}

	agentID := pickValue(*agentIDFlag, os.Getenv("SUB_AGENT_ID"))
	if agentID == "" {
		agentID = "unknown"
	}

	client := pickValue(*clientFlag, os.Getenv("SUB_CLIENT"))
	if client == "" {
		client = "custom"
	}

	env := pickValue(*envFlag, os.Getenv("SUB_ENV"))
	if env == "" {
		env = "dev"
	}

	principal := pickValue(*principalFlag, os.Getenv("SUB_PRINCIPAL"))

	childEnv := os.Environ()
	childEnv = setEnv(childEnv, "SUB_RUN_ID", runID)
	childEnv = setEnv(childEnv, "SUB_AGENT_ID", agentID)
	childEnv = setEnv(childEnv, "SUB_CLIENT", client)
	childEnv = setEnv(childEnv, "SUB_ENV", env)
	if principal != "" {
		childEnv = setEnv(childEnv, "SUB_PRINCIPAL", principal)
	}

	if *printRunID {
		fmt.Fprintln(os.Stdout, runID)
	}

	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Env = childEnv
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "run error: %v\n", err)
		return 1
	}
	return 0
}

func pickValue(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return strings.TrimSpace(fallback)
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
