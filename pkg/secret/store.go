package secret

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const secretsPathEnvVar = "SUB_SECRETS_PATH"

// Entry represents a stored secret value.
type Entry struct {
	Value     string `json:"value"`
	Source    string `json:"source,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// Store maps secret refs to stored entries.
type Store map[string]Entry

// ResolveStorePath returns the store path, honoring env overrides.
func ResolveStorePath() (string, error) {
	if path := strings.TrimSpace(os.Getenv(secretsPathEnvVar)); path != "" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".subluminal", "secrets.json"), nil
}

// LoadStore reads the secrets store from disk. Missing files return an empty store.
func LoadStore(path string) (Store, error) {
	store := Store{}
	if path == "" {
		return store, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	return store, nil
}

// SaveStore writes the secrets store to disk with restrictive permissions.
func SaveStore(path string, store Store) error {
	if path == "" {
		return errors.New("secrets store path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

// NewEntry creates a store entry with updated timestamp.
func NewEntry(value, source string) Entry {
	return Entry{
		Value:     value,
		Source:    source,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}
