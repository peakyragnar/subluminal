package secret

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

const (
	bindingsEnvVar     = "SUB_SECRET_BINDINGS"
	bindingsFileEnvVar = "SUB_SECRET_BINDINGS_FILE"
	defaultSource      = "env"
)

// Binding defines how to inject a secret into the upstream process.
type Binding struct {
	InjectAs  string `json:"inject_as"`
	SecretRef string `json:"secret_ref"`
	Source    string `json:"source,omitempty"`
	Redact    *bool  `json:"redact,omitempty"`
}

// ServerBindings groups bindings for a specific server.
type ServerBindings struct {
	ServerName     string    `json:"server_name"`
	SecretBindings []Binding `json:"secret_bindings"`
}

// LoadBindingsFromEnv loads secret bindings for the given server from env or file.
func LoadBindingsFromEnv(serverName string) ([]Binding, error) {
	raw := strings.TrimSpace(os.Getenv(bindingsEnvVar))
	if path := strings.TrimSpace(os.Getenv(bindingsFileEnvVar)); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		raw = strings.TrimSpace(string(data))
	}
	if raw == "" {
		return nil, nil
	}
	return ParseBindings([]byte(raw), serverName)
}

// ParseBindings parses secret bindings JSON and filters for the given server name.
func ParseBindings(raw []byte, serverName string) ([]Binding, error) {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}

	switch root.(type) {
	case []any:
		if bindings, ok, err := parseServerBindingsArray(raw, serverName); err != nil {
			return nil, err
		} else if ok {
			return bindings, nil
		}
		if bindings, ok, err := parseBindingsArray(raw); err != nil {
			return nil, err
		} else if ok {
			return bindings, nil
		}
	case map[string]any:
		if bindings, ok, err := parseServerBindingsObject(raw, serverName); err != nil {
			return nil, err
		} else if ok {
			return bindings, nil
		}
		if bindings, ok, err := parseBindingsMap(raw, serverName); err != nil {
			return nil, err
		} else if ok {
			return bindings, nil
		}
	}

	return nil, errors.New("invalid secret bindings config")
}

func parseServerBindingsArray(raw []byte, serverName string) ([]Binding, bool, error) {
	var configs []ServerBindings
	if err := json.Unmarshal(raw, &configs); err != nil || !hasServerBindings(configs) {
		return nil, false, nil
	}
	for _, cfg := range configs {
		if cfg.ServerName == "" || cfg.ServerName == serverName {
			normalized, err := normalizeBindings(cfg.SecretBindings)
			if err != nil {
				return nil, true, err
			}
			return normalized, true, nil
		}
	}
	return nil, true, nil
}

func parseServerBindingsObject(raw []byte, serverName string) ([]Binding, bool, error) {
	var cfg ServerBindings
	if err := json.Unmarshal(raw, &cfg); err != nil || len(cfg.SecretBindings) == 0 {
		return nil, false, nil
	}
	if cfg.ServerName != "" && cfg.ServerName != serverName {
		return nil, true, nil
	}
	normalized, err := normalizeBindings(cfg.SecretBindings)
	if err != nil {
		return nil, true, err
	}
	return normalized, true, nil
}

func parseBindingsMap(raw []byte, serverName string) ([]Binding, bool, error) {
	var byServer map[string][]Binding
	if err := json.Unmarshal(raw, &byServer); err != nil || len(byServer) == 0 {
		return nil, false, nil
	}
	if bindings, ok := byServer[serverName]; ok {
		normalized, err := normalizeBindings(bindings)
		if err != nil {
			return nil, true, err
		}
		return normalized, true, nil
	}
	return nil, true, nil
}

func parseBindingsArray(raw []byte) ([]Binding, bool, error) {
	var bindings []Binding
	if err := json.Unmarshal(raw, &bindings); err != nil || !hasBindings(bindings) {
		return nil, false, nil
	}
	normalized, err := normalizeBindings(bindings)
	if err != nil {
		return nil, true, err
	}
	return normalized, true, nil
}

func hasServerBindings(configs []ServerBindings) bool {
	for _, cfg := range configs {
		if cfg.ServerName != "" || len(cfg.SecretBindings) > 0 {
			return true
		}
	}
	return false
}

func hasBindings(bindings []Binding) bool {
	for _, binding := range bindings {
		if binding.InjectAs != "" || binding.SecretRef != "" {
			return true
		}
	}
	return false
}

func normalizeBindings(bindings []Binding) ([]Binding, error) {
	normalized := make([]Binding, 0, len(bindings))
	var invalid bool
	for _, binding := range bindings {
		normalizedBinding, ok := normalizeBinding(binding)
		if !ok {
			invalid = true
			continue
		}
		normalized = append(normalized, normalizedBinding)
	}
	if invalid {
		return normalized, errors.New("invalid secret binding entry")
	}
	return normalized, nil
}

func normalizeBinding(binding Binding) (Binding, bool) {
	injectAs := strings.TrimSpace(binding.InjectAs)
	secretRef := strings.TrimSpace(binding.SecretRef)
	if injectAs == "" || secretRef == "" {
		return Binding{}, false
	}
	source := strings.ToLower(strings.TrimSpace(binding.Source))
	if source == "" {
		source = defaultSource
	}
	redact := true
	if binding.Redact != nil {
		redact = *binding.Redact
	}
	return Binding{
		InjectAs:  injectAs,
		SecretRef: secretRef,
		Source:    source,
		Redact:    &redact,
	}, true
}
