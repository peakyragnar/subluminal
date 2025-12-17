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
//
// The server reads JSON-RPC from stdin and writes responses to stdout.
// It responds to: initialize, tools/list, tools/call
package main

import (
	"flag"
	"os"
	"strings"

	"github.com/subluminal/subluminal/pkg/testharness"
)

func main() {
	// Parse flags
	toolsFlag := flag.String("tools", "test_tool", "Comma-separated list of tool names to expose")
	echoMode := flag.Bool("echo", false, "Echo mode: return args JSON as result")
	crashOn := flag.String("crash-on", "", "Exit immediately when this tool is called (simulate crash)")
	flag.Parse()

	// Create server
	server := testharness.NewFakeMCPServer()

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

func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return "\"" + val + "\""
	case float64:
		if val == float64(int64(val)) {
			return strings.TrimSuffix(strings.TrimSuffix(string(rune(int(val)+'0')), ".0"), ".0")
		}
		return "number"
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
