// Package main implements a standalone fake MCP server for testing.
//
// This is a wrapper around pkg/testharness.FakeMCPServer that can be
// executed as a subprocess by the shim during testing.
//
// Usage:
//
//	./fakemcp                        # Server with default "test_tool"
//	./fakemcp --tools=git_push,list  # Server with specific tools
//	./fakemcp --echo                 # Echo mode: return args as result
//	./fakemcp --crash-on=toolname    # Exit(1) when toolname is called (simulate crash)
//	./fakemcp --error-on=toolname    # Return JSON-RPC error when toolname is called
//	./fakemcp --require-env=VAR      # Require env var(s) for tool calls
//
// The server reads JSON-RPC from stdin and writes responses to stdout.
// It responds to: initialize, tools/list, tools/call
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/subluminal/subluminal/pkg/testharness"
)

func main() {
	// Parse flags
	toolsFlag := flag.String("tools", "test_tool", "Comma-separated list of tool names to expose")
	echoMode := flag.Bool("echo", false, "Echo mode: return args JSON as result")
	measureSize := flag.Bool("measure-size", false, "Measure-size mode: return {\"bytes_received\": N} where N is the JSON size of args")
	crashOn := flag.String("crash-on", "", "Exit immediately when this tool is called (simulate crash)")
	errorOn := flag.String("error-on", "", "Return error when this tool is called (comma-separated)")
	requireEnv := flag.String("require-env", "", "Require env vars for tool calls (comma-separated)")
	flag.Parse()

	// Parse error-on tools into a set
	errorTools := make(map[string]bool)
	if *errorOn != "" {
		for _, name := range strings.Split(*errorOn, ",") {
			errorTools[strings.TrimSpace(name)] = true
		}
	}

	// Create server
	server := testharness.NewFakeMCPServer()
	if *requireEnv != "" {
		for _, name := range strings.Split(*requireEnv, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			server.RequireEnv = append(server.RequireEnv, name)
		}
	}

	// Parse tool names
	toolNames := strings.Split(*toolsFlag, ",")
	for _, name := range toolNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		if name == *crashOn {
			// Crash mode: exit immediately when this tool is called
			server.AddTool(name, "Test tool (crashes)", func(args map[string]any) (string, error) {
				os.Exit(1) // Simulate crash - no response sent
				return "", nil
			})
		} else if errorTools[name] {
			// Error mode: return an error for this tool
			server.AddTool(name, "Test tool (errors)", func(args map[string]any) (string, error) {
				return "", errors.New("simulated tool error")
			})
		} else if *measureSize {
			// Measure-size mode: return the byte size of the args JSON
			server.AddTool(name, "Test tool (measure-size mode)", measureSizeHandler)
		} else if *echoMode {
			// Echo mode: return the arguments as the result
			server.AddTool(name, "Test tool (echo mode)", echoHandler)
		} else {
			// Default mode: return "ok"
			server.AddTool(name, "Test tool", nil)
		}
	}

	// Run server (blocks until stdin closes)
	server.Run(os.Stdin, os.Stdout)
}

// echoHandler returns the arguments as a JSON string.
func echoHandler(args map[string]any) (string, error) {
	if args == nil || len(args) == 0 {
		return "{}", nil
	}
	// Simple string representation
	result := "{"
	first := true
	for k, v := range args {
		if !first {
			result += ", "
		}
		first = false
		result += k + ": " + formatValue(v)
	}
	result += "}"
	return result, nil
}

// measureSizeHandler returns the byte size of the args JSON.
// This is useful for BUF-003 test to verify large payloads are forwarded correctly.
func measureSizeHandler(args map[string]any) (string, error) {
	// Marshal args to JSON to get the exact byte size
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("failed to marshal args: %w", err)
	}

	// Return the byte count
	return fmt.Sprintf(`{"bytes_received": %d}`, len(argsJSON)), nil
}

func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return "\"" + val + "\""
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'g', -1, 64)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	default:
		return "value"
	}
}
