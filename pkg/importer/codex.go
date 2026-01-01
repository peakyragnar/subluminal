package importer

import "path/filepath"

func codexConfigCandidates(homeDir, configDir string) []string {
	candidates := []string{
		filepath.Join(homeDir, ".config", "codex", "mcp.json"),
		filepath.Join(homeDir, ".config", "codex", "config.json"),
		filepath.Join(homeDir, ".config", "openai", "codex", "mcp.json"),
		filepath.Join(homeDir, ".config", "openai", "codex", "config.json"),
	}
	if configDir != "" {
		candidates = append(candidates,
			filepath.Join(configDir, "codex", "mcp.json"),
			filepath.Join(configDir, "codex", "config.json"),
			filepath.Join(configDir, "openai", "codex", "mcp.json"),
			filepath.Join(configDir, "openai", "codex", "config.json"),
		)
	}
	return dedupePaths(candidates)
}
