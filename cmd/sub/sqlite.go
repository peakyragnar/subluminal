package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const sqliteSeparator = "\t"

func defaultLedgerPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".subluminal", "ledger.db"), nil
}

func resolveLedgerPath(path string) (string, error) {
	if path == "" {
		return defaultLedgerPath()
	}
	return expandPath(path)
}

func expandPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func ensureSQLiteAvailable() error {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return fmt.Errorf("sqlite3 not found: %w", err)
	}
	return nil
}

func ensureLedgerExists(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("ledger db not found at %s", path)
		}
		return fmt.Errorf("ledger db error: %w", err)
	}
	return nil
}

func runSQLiteQuery(dbPath, query string) (string, error) {
	if strings.TrimSpace(dbPath) == "" {
		return "", fmt.Errorf("db path is required")
	}

	cmd := exec.Command("sqlite3", "-batch", "-noheader", "-separator", sqliteSeparator, dbPath, query)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("sqlite3 failed: %w: %s", err, strings.TrimSpace(output.String()))
	}
	return output.String(), nil
}

func sqlText(value string) string {
	if value == "" {
		return "NULL"
	}
	escaped := strings.ReplaceAll(value, "'", "''")
	return "'" + escaped + "'"
}
