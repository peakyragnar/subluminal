package importer

import (
	"fmt"
	"os"
)

const backupSuffix = ".subluminal.bak"

func backupPath(configPath string) string {
	return configPath + backupSuffix
}

func readFileWithPerm(path string) ([]byte, os.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	return data, info.Mode().Perm(), nil
}

func writeBackupIfMissing(configPath string, data []byte, perm os.FileMode) (string, bool, error) {
	path := backupPath(configPath)
	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	} else if !os.IsNotExist(err) {
		return path, false, fmt.Errorf("stat backup: %w", err)
	}

	if err := os.WriteFile(path, data, perm); err != nil {
		return path, false, fmt.Errorf("write backup: %w", err)
	}
	return path, true, nil
}

func restoreFromBackup(configPath string) (string, error) {
	path := backupPath(configPath)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return path, fmt.Errorf("backup not found at %s", path)
		}
		return path, fmt.Errorf("stat backup: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return path, fmt.Errorf("read backup: %w", err)
	}

	if err := os.WriteFile(configPath, data, info.Mode().Perm()); err != nil {
		return path, fmt.Errorf("restore config: %w", err)
	}
	return path, nil
}
