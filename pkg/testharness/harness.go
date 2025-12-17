// Package testharness provides test infrastructure for contract testing.
//
// This file implements the test harness orchestrator that ties everything together.
//
// WHY THIS EXISTS:
// Running a contract test requires:
//   - Starting a fake MCP server
//   - Starting the shim (the thing we're testing)
//   - Connecting an agent driver to send requests
//   - Capturing events from the shim
//   - Cleaning up when done
//
// This harness manages all of that, providing a simple API for tests.
//
// NOTE: The shim doesn't exist yet! We'll build it with agents.
// For now, tests can run in "direct" mode (driver → fake server, no shim)
// to verify the harness itself works.
package testharness

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// =============================================================================
// TestHarness orchestrates a complete test environment.
// =============================================================================

// TestHarness manages the lifecycle of test components.
type TestHarness struct {
	// Config
	config HarnessConfig

	// Components
	FakeServer *FakeMCPServer
	EventSink  *EventSink
	Driver     *AgentDriver

	// Process management (when running real shim)
	shimCmd    *exec.Cmd
	shimStdin  io.WriteCloser
	shimStdout io.ReadCloser

	// Pipes for direct mode (no shim)
	directPipes *directPipes

	// State
	running bool
	mu      sync.Mutex
}

// HarnessConfig configures the test harness.
type HarnessConfig struct {
	// ShimPath is the path to the shim binary.
	// If empty, runs in "direct" mode (driver → fake server).
	ShimPath string

	// ShimArgs are additional arguments to pass to the shim.
	ShimArgs []string

	// ShimEnv are environment variables for the shim process.
	ShimEnv []string

	// Timeout is how long to wait for operations.
	Timeout time.Duration
}

// directPipes connects driver directly to fake server (no shim).
type directPipes struct {
	// driver writes here, fake server reads
	driverToServer *io.PipeWriter
	serverFromDriver *io.PipeReader

	// fake server writes here, driver reads
	serverToDriver *io.PipeWriter
	driverFromServer *io.PipeReader
}

// NewTestHarness creates a new test harness with the given config.
func NewTestHarness(config HarnessConfig) *TestHarness {
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}

	return &TestHarness{
		config:     config,
		FakeServer: NewFakeMCPServer(),
		EventSink:  NewEventSink(),
	}
}

// NewDirectHarness creates a harness in direct mode (no shim).
// Use this to test the harness itself or when shim doesn't exist yet.
func NewDirectHarness() *TestHarness {
	return NewTestHarness(HarnessConfig{})
}

// =============================================================================
// Lifecycle methods
// =============================================================================

// Start initializes and starts all components.
func (h *TestHarness) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.running {
		return fmt.Errorf("harness already running")
	}

	if h.config.ShimPath != "" {
		return h.startWithShim()
	}
	return h.startDirect()
}

// startDirect runs in direct mode: driver connects directly to fake server.
func (h *TestHarness) startDirect() error {
	// Create pipes
	serverFromDriver, driverToServer := io.Pipe()
	driverFromServer, serverToDriver := io.Pipe()

	h.directPipes = &directPipes{
		driverToServer:   driverToServer,
		serverFromDriver: serverFromDriver,
		serverToDriver:   serverToDriver,
		driverFromServer: driverFromServer,
	}

	// Start fake server in goroutine
	go func() {
		h.FakeServer.Run(serverFromDriver, serverToDriver)
	}()

	// Create driver connected to fake server
	h.Driver = NewAgentDriver(driverToServer, driverFromServer)
	h.Driver.StartResponseReader()

	h.running = true
	return nil
}

// startWithShim starts the real shim process.
// The shim spawns its own upstream MCP server (fakemcp) as a subprocess.
func (h *TestHarness) startWithShim() error {
	// Get tool names from the fake server config
	toolNames := h.getToolNames()

	// Build shim args: --server-name=test -- ./bin/fakemcp --tools=tool1,tool2
	args := append([]string{}, h.config.ShimArgs...)
	if !hasServerName(args) {
		args = append(args, "--server-name=test")
	}
	args = append(args, "--")
	args = append(args, h.getFakeMCPPath())
	if len(toolNames) > 0 {
		args = append(args, "--tools="+joinTools(toolNames))
	}

	// Start shim process
	h.shimCmd = exec.Command(h.config.ShimPath, args...)
	h.shimCmd.Env = append(os.Environ(), h.config.ShimEnv...)

	// Connect shim stdin/stdout
	var err error
	h.shimStdin, err = h.shimCmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get shim stdin: %w", err)
	}

	h.shimStdout, err = h.shimCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get shim stdout: %w", err)
	}

	// Capture shim stderr for events
	shimStderr, err := h.shimCmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get shim stderr: %w", err)
	}

	// Start event capture from stderr (where shim emits events)
	go h.EventSink.Capture(shimStderr)

	// Start shim (shim will spawn fakemcp as its upstream)
	if err := h.shimCmd.Start(); err != nil {
		return fmt.Errorf("failed to start shim: %w", err)
	}

	// Create driver connected to shim
	h.Driver = NewAgentDriver(h.shimStdin, h.shimStdout)
	h.Driver.StartResponseReader()

	h.running = true
	return nil
}

// getToolNames extracts tool names from the fake server.
func (h *TestHarness) getToolNames() []string {
	if h.FakeServer == nil {
		return nil
	}
	names := make([]string, len(h.FakeServer.Tools))
	for i, t := range h.FakeServer.Tools {
		names[i] = t.Name
	}
	return names
}

// getFakeMCPPath returns the path to the fakemcp binary.
func (h *TestHarness) getFakeMCPPath() string {
	// Check environment override
	if p := os.Getenv("SUBLUMINAL_FAKEMCP_PATH"); p != "" {
		return p
	}
	// Default: relative to shim binary
	return "./bin/fakemcp"
}

// hasServerName checks if --server-name is already in args.
func hasServerName(args []string) bool {
	for _, arg := range args {
		if len(arg) > 13 && arg[:13] == "--server-name" {
			return true
		}
	}
	return false
}

// joinTools joins tool names with commas.
func joinTools(tools []string) string {
	result := ""
	for i, t := range tools {
		if i > 0 {
			result += ","
		}
		result += t
	}
	return result
}

// Stop shuts down all components and cleans up.
func (h *TestHarness) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.running {
		return nil
	}

	var errs []error

	// Close driver
	if h.Driver != nil {
		if err := h.Driver.Close(); err != nil {
			errs = append(errs, fmt.Errorf("driver close: %w", err))
		}
	}

	// Stop shim process
	if h.shimCmd != nil && h.shimCmd.Process != nil {
		h.shimCmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() {
			done <- h.shimCmd.Wait()
		}()

		select {
		case <-done:
			// Clean exit
		case <-time.After(h.config.Timeout):
			h.shimCmd.Process.Kill()
		}
	}

	// Close direct mode pipes
	if h.directPipes != nil {
		h.directPipes.driverToServer.Close()
		h.directPipes.serverToDriver.Close()
	}

	h.running = false

	if len(errs) > 0 {
		return fmt.Errorf("errors during stop: %v", errs)
	}
	return nil
}

// =============================================================================
// Convenience methods for tests
// =============================================================================

// AddTool is a shortcut to add a tool to the fake server.
func (h *TestHarness) AddTool(name, description string, handler ToolHandler) {
	h.FakeServer.AddTool(name, description, handler)
}

// CallTool is a shortcut to make a tool call via the driver.
func (h *TestHarness) CallTool(name string, args map[string]any) (*JSONRPCResponse, error) {
	if h.Driver == nil {
		return nil, fmt.Errorf("harness not started")
	}
	return h.Driver.CallTool(name, args)
}

// Initialize is a shortcut to initialize the MCP connection.
func (h *TestHarness) Initialize() error {
	if h.Driver == nil {
		return fmt.Errorf("harness not started")
	}
	_, err := h.Driver.Initialize()
	return err
}

// Events returns all captured events.
func (h *TestHarness) Events() []CapturedEvent {
	return h.EventSink.All()
}

// =============================================================================
// Test assertion helpers
// =============================================================================

// AssertEventOrder checks event sequence (delegates to EventSink).
func (h *TestHarness) AssertEventOrder(types ...string) error {
	return h.EventSink.AssertEventOrder(types...)
}

// AssertAllEventsHaveField checks a field exists in all events.
func (h *TestHarness) AssertAllEventsHaveField(field string) error {
	return h.EventSink.AssertAllHaveField(field)
}

// AssertRunIDConsistent checks run_id is same across all events.
func (h *TestHarness) AssertRunIDConsistent() error {
	return h.EventSink.AssertFieldConsistent("run_id")
}

// =============================================================================
// RunTest is a high-level helper that sets up, runs, and tears down.
// =============================================================================

// TestFunc is a function that runs test logic with a started harness.
type TestFunc func(h *TestHarness) error

// RunTest creates a harness, starts it, runs the test, and stops it.
// This is the recommended way to write contract tests.
//
// Example:
//
//	err := RunTest(HarnessConfig{}, func(h *TestHarness) error {
//	    h.AddTool("git_push", "Push to git", nil)
//	    h.Initialize()
//	    h.CallTool("git_push", map[string]any{"branch": "main"})
//	    return h.AssertEventOrder("run_start", "tool_call_end", "run_end")
//	})
func RunTest(config HarnessConfig, fn TestFunc) error {
	h := NewTestHarness(config)

	if err := h.Start(); err != nil {
		return fmt.Errorf("failed to start harness: %w", err)
	}
	defer h.Stop()

	return fn(h)
}

// RunDirectTest runs a test in direct mode (no shim).
func RunDirectTest(fn TestFunc) error {
	return RunTest(HarnessConfig{}, fn)
}
