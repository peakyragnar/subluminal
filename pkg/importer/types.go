package importer

import (
	"fmt"
	"strings"
)

// Client identifies the MCP client type to import from.
type Client string

const (
	ClientClaude Client = "claude"
	ClientCodex  Client = "codex"
)

// Options configures import or restore operations.
type Options struct {
	Client     Client
	ConfigPath string
	ShimPath   string
}

// ImportResult describes the outcome of an import operation.
type ImportResult struct {
	Client      Client
	ConfigPath  string
	BackupPath  string
	ServerNames []string
	Changed     bool
}

// ParseClient normalizes a client string into a supported Client.
func ParseClient(raw string) (Client, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ClientClaude):
		return ClientClaude, nil
	case string(ClientCodex):
		return ClientCodex, nil
	default:
		return "", fmt.Errorf("unsupported client %q (expected claude or codex)", raw)
	}
}
