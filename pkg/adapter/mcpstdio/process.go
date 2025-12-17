// Package mcpstdio implements the MCP stdio adapter.
//
// This file handles upstream process management:
// - Spawning the upstream MCP server as a subprocess
// - Signal forwarding (SIGINT, SIGTERM)
// - Clean shutdown on EOF
//
// Per Interface-Pack §7.4:
// - Adapters handle transport-specific process lifecycle
package mcpstdio

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// UpstreamProcess manages a subprocess running an MCP server.
type UpstreamProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	// Configuration
	command string
	args    []string
	env     []string
}

// NewUpstreamProcess creates a new upstream process manager.
// Does not start the process - call Start() to begin.
func NewUpstreamProcess(command string, args []string) *UpstreamProcess {
	return &UpstreamProcess{
		command: command,
		args:    args,
		env:     os.Environ(), // Inherit environment by default
	}
}

// SetEnv sets additional environment variables for the upstream process.
// These are appended to the inherited environment.
func (up *UpstreamProcess) SetEnv(env []string) {
	up.env = append(os.Environ(), env...)
}

// Start launches the upstream process.
func (up *UpstreamProcess) Start() error {
	up.cmd = exec.Command(up.command, up.args...)
	up.cmd.Env = up.env

	// Set process group so we can signal the whole group
	up.cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Get pipes
	var err error
	up.stdin, err = up.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	up.stdout, err = up.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	up.stderr, err = up.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Start the process
	if err := up.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start upstream process: %w", err)
	}

	return nil
}

// Stdin returns the write end of the upstream's stdin pipe.
func (up *UpstreamProcess) Stdin() io.WriteCloser {
	return up.stdin
}

// Stdout returns the read end of the upstream's stdout pipe.
func (up *UpstreamProcess) Stdout() io.ReadCloser {
	return up.stdout
}

// Stderr returns the read end of the upstream's stderr pipe.
func (up *UpstreamProcess) Stderr() io.ReadCloser {
	return up.stderr
}

// Signal sends a signal to the upstream process group.
func (up *UpstreamProcess) Signal(sig os.Signal) error {
	if up.cmd == nil || up.cmd.Process == nil {
		return nil
	}

	// Send to process group (negative PID)
	pgid, err := syscall.Getpgid(up.cmd.Process.Pid)
	if err != nil {
		// Fallback to sending to process directly
		return up.cmd.Process.Signal(sig)
	}

	return syscall.Kill(-pgid, sig.(syscall.Signal))
}

// Wait waits for the upstream process to exit.
// Returns the process state or error.
func (up *UpstreamProcess) Wait() (*os.ProcessState, error) {
	if up.cmd == nil {
		return nil, fmt.Errorf("process not started")
	}
	return up.cmd.Process.Wait()
}

// Stop gracefully stops the upstream process.
// Sends SIGTERM, waits for timeout, then SIGKILL if needed.
func (up *UpstreamProcess) Stop(timeout time.Duration) error {
	if up.cmd == nil || up.cmd.Process == nil {
		return nil
	}

	// Close stdin to signal EOF
	if up.stdin != nil {
		up.stdin.Close()
	}

	// Send SIGTERM
	if err := up.Signal(syscall.SIGTERM); err != nil {
		// Process might already be gone
		return nil
	}

	// Wait for exit with timeout
	done := make(chan error, 1)
	go func() {
		_, err := up.cmd.Process.Wait()
		done <- err
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		// Force kill
		up.Signal(syscall.SIGKILL)
		<-done
		return nil
	}
}

// CloseStdin closes the upstream's stdin pipe.
// This signals EOF to the upstream process.
func (up *UpstreamProcess) CloseStdin() error {
	if up.stdin != nil {
		return up.stdin.Close()
	}
	return nil
}

// Pid returns the process ID of the upstream process, or -1 if not started.
func (up *UpstreamProcess) Pid() int {
	if up.cmd == nil || up.cmd.Process == nil {
		return -1
	}
	return up.cmd.Process.Pid
}
