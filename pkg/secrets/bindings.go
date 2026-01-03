package secrets

import "fmt"

// SecretSource describes where a secret is loaded from.
type SecretSource string

const (
	SecretSourceEnv      SecretSource = "env"
	SecretSourceKeychain SecretSource = "keychain"
	SecretSourceFile     SecretSource = "file"
)

const (
	DefaultSecretSource = SecretSourceEnv
	DefaultSecretRedact = true
)

// SecretBinding defines how a secret should be injected into the upstream env.
type SecretBinding struct {
	SecretRef string
	Source    SecretSource
	Redact    bool
}

// SecretBindings maps inject_as env var names to secret bindings.
type SecretBindings map[string]SecretBinding

// ParseSecretBindings parses a secret_bindings config value into bindings.
// Accepts either:
// - object: { "ENV_VAR": "secret_ref" }
// - array: [{inject_as, secret_ref, source?, redact?}]
func ParseSecretBindings(raw any) (SecretBindings, error) {
	if raw == nil {
		return nil, nil
	}

	switch value := raw.(type) {
	case []any:
		return parseSecretBindingsArray(value)
	case map[string]any:
		return parseSecretBindingsMap(value)
	case map[string]string:
		bindings := make(SecretBindings, len(value))
		for injectAs, secretRef := range value {
			if injectAs == "" {
				return nil, fmt.Errorf("secret_bindings key is empty")
			}
			if secretRef == "" {
				return nil, fmt.Errorf("secret_bindings[%s] missing secret_ref", injectAs)
			}
			bindings[injectAs] = SecretBinding{
				SecretRef: secretRef,
				Source:    DefaultSecretSource,
				Redact:    DefaultSecretRedact,
			}
		}
		return bindings, nil
	default:
		return nil, fmt.Errorf("secret_bindings must be an array or object")
	}
}

// ParseServerSecretBindings reads secret_bindings from a server config object.
func ParseServerSecretBindings(server map[string]any) (SecretBindings, error) {
	if server == nil {
		return nil, nil
	}
	return ParseSecretBindings(server["secret_bindings"])
}

// EnvInjectionMap converts bindings to an env injection map for the shim.
// Returns an error if any binding uses a non-env source.
func EnvInjectionMap(bindings SecretBindings) (map[string]string, error) {
	if len(bindings) == 0 {
		return nil, nil
	}

	env := make(map[string]string, len(bindings))
	for injectAs, binding := range bindings {
		source := binding.Source
		if source == "" {
			source = DefaultSecretSource
		}
		if source != SecretSourceEnv {
			return nil, fmt.Errorf("secret_bindings[%s] unsupported source %q", injectAs, source)
		}
		if binding.SecretRef == "" {
			return nil, fmt.Errorf("secret_bindings[%s] missing secret_ref", injectAs)
		}
		env[injectAs] = binding.SecretRef
	}
	return env, nil
}

func parseSecretBindingsMap(raw map[string]any) (SecretBindings, error) {
	bindings := make(SecretBindings, len(raw))
	for injectAs, rawSecretRef := range raw {
		if injectAs == "" {
			return nil, fmt.Errorf("secret_bindings key is empty")
		}
		secretRef, ok := rawSecretRef.(string)
		if !ok {
			return nil, fmt.Errorf("secret_bindings[%s] must be a string", injectAs)
		}
		if secretRef == "" {
			return nil, fmt.Errorf("secret_bindings[%s] missing secret_ref", injectAs)
		}
		bindings[injectAs] = SecretBinding{
			SecretRef: secretRef,
			Source:    DefaultSecretSource,
			Redact:    DefaultSecretRedact,
		}
	}
	return bindings, nil
}

func parseSecretBindingsArray(raw []any) (SecretBindings, error) {
	bindings := make(SecretBindings, len(raw))
	for i, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("secret_bindings[%d] must be an object", i)
		}

		context := fmt.Sprintf("secret_bindings[%d]", i)

		injectAs, err := requireString(entry, "inject_as", context)
		if err != nil {
			return nil, err
		}

		if _, exists := bindings[injectAs]; exists {
			return nil, fmt.Errorf("%s.inject_as duplicated: %q", context, injectAs)
		}

		secretRef, err := requireString(entry, "secret_ref", context)
		if err != nil {
			return nil, err
		}

		source := DefaultSecretSource
		if rawSource, ok := entry["source"]; ok && rawSource != nil {
			text, ok := rawSource.(string)
			if !ok {
				return nil, fmt.Errorf("%s.source must be a string", context)
			}
			if text != "" {
				source = SecretSource(text)
			}
		}
		if !isValidSource(source) {
			return nil, fmt.Errorf("%s.source must be env, keychain, or file", context)
		}

		redact := DefaultSecretRedact
		if rawRedact, ok := entry["redact"]; ok && rawRedact != nil {
			value, ok := rawRedact.(bool)
			if !ok {
				return nil, fmt.Errorf("%s.redact must be a bool", context)
			}
			redact = value
		}

		bindings[injectAs] = SecretBinding{
			SecretRef: secretRef,
			Source:    source,
			Redact:    redact,
		}
	}
	return bindings, nil
}

func requireString(entry map[string]any, field, context string) (string, error) {
	value, ok := entry[field]
	if !ok || value == nil {
		return "", fmt.Errorf("%s.%s is required", context, field)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s.%s must be a string", context, field)
	}
	if text == "" {
		return "", fmt.Errorf("%s.%s is required", context, field)
	}
	return text, nil
}

func isValidSource(source SecretSource) bool {
	switch source {
	case SecretSourceEnv, SecretSourceKeychain, SecretSourceFile:
		return true
	default:
		return false
	}
}
