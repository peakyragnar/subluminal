package policy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/subluminal/subluminal/pkg/canonical"
	"github.com/subluminal/subluminal/pkg/event"
)

// LoadBundleFile reads a policy bundle from disk.
func LoadBundleFile(path string) (BundleSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BundleSpec{}, err
	}
	return ParseBundle(data)
}

// ParseBundle parses a JSON or YAML policy bundle payload.
func ParseBundle(data []byte) (BundleSpec, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return BundleSpec{}, fmt.Errorf("empty policy bundle")
	}

	var raw any
	var err error
	switch trimmed[0] {
	case '{':
		raw, err = parseJSONBundle(trimmed)
	default:
		raw, err = parseYAMLBundle(string(data))
	}
	if err != nil {
		return BundleSpec{}, err
	}

	return decodeBundle(raw)
}

func parseJSONBundle(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("unexpected trailing JSON data")
	}
	return raw, nil
}

func decodeBundle(raw any) (BundleSpec, error) {
	if _, ok := raw.(map[string]any); !ok {
		return BundleSpec{}, fmt.Errorf("policy bundle must be a JSON/YAML object")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return BundleSpec{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(encoded))
	dec.UseNumber()
	var spec BundleSpec
	if err := dec.Decode(&spec); err != nil {
		return BundleSpec{}, err
	}
	return spec, nil
}

type policySnapshot struct {
	PolicyID      string          `json:"policy_id"`
	PolicyVersion string          `json:"policy_version"`
	Mode          string          `json:"mode"`
	Defaults      PolicyDefaults  `json:"defaults,omitempty"`
	Selectors     PolicySelectors `json:"selectors,omitempty"`
	Rules         []Rule          `json:"rules"`
}

// CompileBundle compiles a bundle spec into a runtime bundle + snapshot hash.
func CompileBundle(spec BundleSpec) (CompiledBundle, error) {
	policyID := defaultString(spec.PolicyID, "default")
	version := defaultString(spec.EffectiveVersion(), "0.1.0")
	mode := normalizeModeString(spec.Mode)

	snapshot := buildSnapshot(policyID, version, mode, spec.Defaults, spec.Selectors, spec.Rules)
	hash, snapshotBytes, err := hashSnapshot(snapshot)
	if err != nil {
		return CompiledBundle{}, err
	}

	bundle := Bundle{
		Mode: parseMode(mode),
		Info: event.PolicyInfo{
			PolicyID:      policyID,
			PolicyVersion: version,
			PolicyHash:    hash,
		},
		Defaults:  spec.Defaults,
		Selectors: spec.Selectors,
		Rules:     spec.Rules,
	}
	bundle.ensureState()

	return CompiledBundle{
		Bundle:   bundle,
		Snapshot: snapshotBytes,
		Hash:     hash,
	}, nil
}

func buildSnapshot(policyID, version, mode string, defaults PolicyDefaults, selectors PolicySelectors, rules []Rule) policySnapshot {
	return policySnapshot{
		PolicyID:      policyID,
		PolicyVersion: version,
		Mode:          mode,
		Defaults:      defaults,
		Selectors:     selectors,
		Rules:         rules,
	}
}

func normalizeModeString(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "guardrails":
		return "guardrails"
	case "control":
		return "control"
	default:
		return "observe"
	}
}

func hashSnapshot(snapshot policySnapshot) (string, []byte, error) {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(encoded))
	dec.UseNumber()
	var normalized any
	if err := dec.Decode(&normalized); err != nil {
		return "", nil, err
	}
	canonicalBytes, err := canonical.Canonicalize(normalized)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(canonicalBytes)
	return hex.EncodeToString(sum[:]), canonicalBytes, nil
}

// LoadCompiledBundleFile loads, validates, and compiles a bundle from disk.
func LoadCompiledBundleFile(path string) (CompiledBundle, error) {
	spec, err := LoadBundleFile(path)
	if err != nil {
		return CompiledBundle{}, err
	}
	if lintErrs := lintErrors(LintBundle(spec)); len(lintErrs) > 0 {
		return CompiledBundle{}, fmt.Errorf("policy validation failed: %s", formatLintIssues(lintErrs))
	}
	return CompileBundle(spec)
}

func lintErrors(issues []LintIssue) []LintIssue {
	var errs []LintIssue
	for _, issue := range issues {
		if strings.EqualFold(issue.Level, "error") {
			errs = append(errs, issue)
		}
	}
	return errs
}

func formatLintIssues(issues []LintIssue) string {
	if len(issues) == 0 {
		return ""
	}
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		field := strings.TrimSpace(issue.Field)
		if field == "" {
			field = "policy"
		}
		parts = append(parts, fmt.Sprintf("%s: %s", field, issue.Message))
	}
	return strings.Join(parts, "; ")
}

// BundleWatcher polls for policy file changes.
type BundleWatcher struct {
	path        string
	lastModTime time.Time
	lastSize    int64
}

// BundleChange reports a compiled bundle or an error on reload.
type BundleChange struct {
	Compiled CompiledBundle
	Err      error
}

// NewBundleWatcher creates a watcher for the given bundle path.
func NewBundleWatcher(path string) *BundleWatcher {
	return &BundleWatcher{path: path}
}

// Check reloads the bundle if the file has changed since the last check.
func (w *BundleWatcher) Check() (CompiledBundle, bool, error) {
	info, err := os.Stat(w.path)
	if err != nil {
		if os.IsNotExist(err) {
			w.lastModTime = time.Time{}
			w.lastSize = 0
			return CompiledBundle{}, false, nil
		}
		return CompiledBundle{}, false, err
	}

	modTime := info.ModTime()
	size := info.Size()
	if modTime.Equal(w.lastModTime) && size == w.lastSize {
		return CompiledBundle{}, false, nil
	}

	w.lastModTime = modTime
	w.lastSize = size

	compiled, err := LoadCompiledBundleFile(w.path)
	if err != nil {
		return CompiledBundle{}, true, err
	}
	return compiled, true, nil
}

// WatchBundleFile polls a bundle file and emits change events.
func WatchBundleFile(ctx context.Context, path string, interval time.Duration) <-chan BundleChange {
	if interval <= 0 {
		interval = time.Second
	}
	updates := make(chan BundleChange)
	watcher := NewBundleWatcher(path)
	go func() {
		defer close(updates)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				compiled, changed, err := watcher.Check()
				if !changed {
					continue
				}
				updates <- BundleChange{Compiled: compiled, Err: err}
			}
		}
	}()
	return updates
}
