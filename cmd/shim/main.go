// Package main implements the Subluminal shim entry point.
//
// The shim is an MCP stdio proxy that:
// - Reads JSON-RPC from stdin (from agent client)
// - Spawns upstream MCP server as subprocess
// - Forwards requests to upstream, relays responses back
// - Emits JSONL events to stderr for auditing
//
// Usage:
//
//	./shim --server-name=<name> -- <upstream-command> [args...]
//
// Example:
//
//	./shim --server-name=git -- git-mcp-server
//	SUB_AGENT_ID=repo-fixer SUB_ENV=ci ./shim --server-name=git -- git-mcp-server
//
// Environment variables (per Interface-Pack §5):
//
//	SUB_RUN_ID     - Globally unique run ID (generated if not set)
//	SUB_AGENT_ID   - Agent identifier (defaults to "unknown")
//	SUB_CLIENT     - Client type: claude|codex|headless|custom|unknown
//	SUB_ENV        - Environment: dev|ci|prod|unknown
//	SUB_PRINCIPAL  - Optional principal identity (user/service)
//	SUB_WORKLOAD   - Optional JSON object describing workload context
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/subluminal/subluminal/pkg/adapter/mcpstdio"
	"github.com/subluminal/subluminal/pkg/core"
)

func main() {
	// Parse flags
	serverName := flag.String("server-name", "", "Server name for events (required)")
	flag.Parse()

	// Validate required flags
	if *serverName == "" {
		fmt.Fprintln(os.Stderr, "Error: --server-name is required")
		flag.Usage()
		os.Exit(1)
	}

	// Get upstream command (everything after --)
	upstreamArgs := flag.Args()
	if len(upstreamArgs) == 0 {
		fmt.Fprintln(os.Stderr, "Error: upstream command is required after --")
		fmt.Fprintln(os.Stderr, "Usage: shim --server-name=<name> -- <command> [args...]")
		os.Exit(1)
	}

	// Initialize identity from environment
	identity := core.ReadIdentityFromEnv()
	source := core.GenerateSource()

	// Create emitter (writes to stderr)
	emitter := core.NewEmitter(os.Stderr)
	emitter.Start()
	defer emitter.Close()

	// Start upstream process
	upstream := mcpstdio.NewUpstreamProcess(upstreamArgs[0], upstreamArgs[1:])
	if err := upstream.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting upstream: %v\n", err)
		os.Exit(1)
	}

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Create proxy
	proxy := mcpstdio.NewProxy(
		upstream,
		emitter,
		*serverName,
		identity,
		source,
		os.Stdin,
		os.Stdout,
	)

	// Handle signals in background
	go func() {
		sig := <-sigCh
		// Forward signal to upstream
		upstream.Signal(sig)
		// Stop proxy
		proxy.Stop()
	}()

	// Run proxy (blocks until stdin EOF or signal)
	if err := proxy.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Proxy error: %v\n", err)
	}

	// Clean shutdown
	upstream.Stop(5 * time.Second)
}
