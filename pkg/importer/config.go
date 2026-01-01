package importer

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
)

var serverKeys = []string{"mcpServers", "mcp_servers"}

func rewriteConfig(raw []byte, shimPath string) ([]byte, []string, bool, error) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, nil, false, fmt.Errorf("parse config JSON: %w", err)
	}

	servers, key, err := extractServers(root)
	if err != nil {
		return nil, nil, false, err
	}

	serverNames := make([]string, 0, len(servers))
	changed := false
	for name, rawServer := range servers {
		server, ok := rawServer.(map[string]any)
		if !ok {
			return nil, nil, false, fmt.Errorf("server %q is not an object", name)
		}

		command, ok := server["command"].(string)
		if !ok || command == "" {
			return nil, nil, false, fmt.Errorf("server %q missing command", name)
		}

		args, err := parseArgs(server["args"])
		if err != nil {
			return nil, nil, false, fmt.Errorf("server %q args: %w", name, err)
		}

		if isShimWrapped(command, args, shimPath) {
			serverNames = append(serverNames, name)
			continue
		}

		newArgs := []string{"--server-name=" + name, "--", command}
		newArgs = append(newArgs, args...)
		server["command"] = shimPath
		server["args"] = newArgs
		servers[name] = server
		serverNames = append(serverNames, name)
		changed = true
	}

	root[key] = servers
	sort.Strings(serverNames)

	updated, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, nil, false, fmt.Errorf("marshal config JSON: %w", err)
	}
	updated = append(updated, '\n')
	return updated, serverNames, changed, nil
}

func extractServers(root map[string]any) (map[string]any, string, error) {
	for _, key := range serverKeys {
		if raw, ok := root[key]; ok {
			servers, ok := raw.(map[string]any)
			if !ok {
				return nil, "", fmt.Errorf("%s must be an object", key)
			}
			if len(servers) == 0 {
				return nil, "", fmt.Errorf("%s is empty", key)
			}
			return servers, key, nil
		}
	}
	return nil, "", fmt.Errorf("no mcpServers entry found")
}

func parseArgs(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}

	switch v := value.(type) {
	case []string:
		return append([]string{}, v...), nil
	case []any:
		args := make([]string, 0, len(v))
		for i, item := range v {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("args[%d] not a string", i)
			}
			args = append(args, text)
		}
		return args, nil
	default:
		return nil, fmt.Errorf("args must be an array")
	}
}

func isShimWrapped(command string, args []string, shimPath string) bool {
	if command == "" {
		return false
	}

	if command != shimPath && filepath.Base(command) != filepath.Base(shimPath) {
		return false
	}

	for _, arg := range args {
		if arg == "--" {
			return true
		}
	}
	return false
}
