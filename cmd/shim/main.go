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
	"github.com/subluminal/subluminal/pkg/secret"
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

	secretBindings, err := secret.LoadBindingsFromEnv(*serverName)
	if err != nil && os.Getenv("SUB_SECRET_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "Secret bindings error: %v\n", err)
	}

	store := secret.Store{}
	if storePath, err := secret.ResolveStorePath(); err == nil {
		if loaded, err := secret.LoadStore(storePath); err == nil {
			store = loaded
		} else if os.Getenv("SUB_SECRET_DEBUG") == "1" {
			fmt.Fprintf(os.Stderr, "Secret store error: %v\n", err)
		}
	} else if os.Getenv("SUB_SECRET_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "Secret store path error: %v\n", err)
	}

	injections := secret.ResolveBindings(secretBindings, store, secret.EnvMap(os.Environ()))
	secretEvents := make([]secret.InjectionEvent, 0, len(injections))
	redactValues := make([]string, 0, len(injections))
	injectEnv := make([]string, 0, len(injections))
	for _, injection := range injections {
		secretEvents = append(secretEvents, injection.Event())
		if injection.Success {
			injectEnv = append(injectEnv, injection.InjectAs+"="+injection.Value)
			if injection.Redact {
				redactValues = append(redactValues, injection.Value)
			}
		}
	}

	redactor := mcpstdio.NewRedactor(redactValues)

	// Start upstream process
	upstream := mcpstdio.NewUpstreamProcess(upstreamArgs[0], upstreamArgs[1:])
	if len(injectEnv) > 0 {
		upstream.SetEnv(injectEnv)
	}
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
		redactor,
		secretEvents,
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
