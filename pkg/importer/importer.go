package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Import rewrites MCP server config entries to route through the shim.
func Import(opts Options) (ImportResult, error) {
	client := opts.Client
	if client == "" {
		return ImportResult{}, fmt.Errorf("client is required")
	}

	configPath, err := resolveConfigPath(opts)
	if err != nil {
		return ImportResult{}, err
	}

	shimPath := resolveShimPath(opts.ShimPath)
	data, perm, err := readFileWithPerm(configPath)
	if err != nil {
		return ImportResult{}, fmt.Errorf("read config: %w", err)
	}

	updated, serverNames, changed, err := rewriteConfig(data, shimPath)
	if err != nil {
		return ImportResult{}, err
	}

	backupFile := backupPath(configPath)
	if changed {
		if _, _, err := writeBackupIfMissing(configPath, data, perm); err != nil {
			return ImportResult{}, err
		}
		if err := os.WriteFile(configPath, updated, perm); err != nil {
			return ImportResult{}, fmt.Errorf("write config: %w", err)
		}
	} else {
		if _, err := os.Stat(backupFile); err != nil {
			if os.IsNotExist(err) {
				backupFile = ""
			} else {
				return ImportResult{}, fmt.Errorf("stat backup: %w", err)
			}
		}
	}

	return ImportResult{
		Client:      client,
		ConfigPath:  configPath,
		BackupPath:  backupFile,
		ServerNames: serverNames,
		Changed:     changed,
	}, nil
}

// Restore replaces the config file with the last backup created by Import.
func Restore(opts Options) (string, error) {
	client := opts.Client
	if client == "" {
		return "", fmt.Errorf("client is required")
	}

	configPath, err := resolveConfigPath(opts)
	if err != nil {
		return "", err
	}

	backupPath, err := restoreFromBackup(configPath)
	if err != nil {
		return backupPath, err
	}
	return backupPath, nil
}

func resolveShimPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if fromEnv := os.Getenv("SUBLUMINAL_SHIM_PATH"); fromEnv != "" {
		return fromEnv
	}
	return "shim"
}

func resolveConfigPath(opts Options) (string, error) {
	if opts.ConfigPath != "" {
		return opts.ConfigPath, nil
	}

	client, err := ParseClient(string(opts.Client))
	if err != nil {
		return "", err
	}

	envVar := "SUBLUMINAL_" + strings.ToUpper(string(client)) + "_CONFIG"
	if fromEnv := os.Getenv(envVar); fromEnv != "" {
		return fromEnv, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}

	configDir, _ := os.UserConfigDir()
	candidates := configCandidates(client, homeDir, configDir)
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("no config candidates for %s", client)
	}
	return "", fmt.Errorf("no %s config found; looked in %s", client, strings.Join(candidates, ", "))
}

func configCandidates(client Client, homeDir, configDir string) []string {
	switch client {
	case ClientClaude:
		return claudeConfigCandidates(homeDir, configDir)
	case ClientCodex:
		return codexConfigCandidates(homeDir, configDir)
	default:
		return nil
	}
}

func dedupePaths(paths []string) []string {
	seen := make(map[string]bool)
	var unique []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		clean := filepath.Clean(p)
		if !seen[clean] {
			seen[clean] = true
			unique = append(unique, clean)
		}
	}
	return unique
}
