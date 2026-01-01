package importer

import "path/filepath"

func claudeConfigCandidates(homeDir, configDir string) []string {
	candidates := []string{
		filepath.Join(homeDir, ".config", "claude-code", "mcp.json"),
		filepath.Join(homeDir, ".config", "claude-code", "config.json"),
		filepath.Join(homeDir, ".config", "Claude", "claude_desktop_config.json"),
	}
	if configDir != "" {
		candidates = append(candidates,
			filepath.Join(configDir, "claude-code", "mcp.json"),
			filepath.Join(configDir, "claude-code", "config.json"),
			filepath.Join(configDir, "Claude", "claude_desktop_config.json"),
		)
	}
	return dedupePaths(candidates)
}
