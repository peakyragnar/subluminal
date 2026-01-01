package importer

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRewriteConfigUpdatesShimPath(t *testing.T) {
	raw := map[string]any{
		"mcpServers": map[string]any{
			"alpha": map[string]any{
				"command": "/old/shim",
				"args":    []any{"--server-name=alpha", "--", "/usr/bin/alpha", "--flag"},
			},
		},
	}

	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	updated, _, changed, err := rewriteConfig(data, "/new/shim")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if !changed {
		t.Fatal("expected config to be marked as changed")
	}

	var out map[string]any
	if err := json.Unmarshal(updated, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	rawServers, ok := out["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers missing or not an object")
	}

	rawServer, ok := rawServers["alpha"].(map[string]any)
	if !ok {
		t.Fatalf("alpha server missing or not an object")
	}

	if rawServer["command"] != "/new/shim" {
		t.Fatalf("expected command to update to new shim path")
	}

	argsRaw, ok := rawServer["args"].([]any)
	if !ok {
		t.Fatalf("args missing or not an array")
	}

	args := make([]string, 0, len(argsRaw))
	for i, arg := range argsRaw {
		text, ok := arg.(string)
		if !ok {
			t.Fatalf("args[%d] not a string", i)
		}
		args = append(args, text)
	}

	expected := []string{"--server-name=alpha", "--", "/usr/bin/alpha", "--flag"}
	if !reflect.DeepEqual(args, expected) {
		t.Fatalf("unexpected args after shim update: %v", args)
	}
}
