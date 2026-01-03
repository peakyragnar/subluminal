package secret

import "strings"

// Injection describes a resolved secret binding.
type Injection struct {
	InjectAs  string
	SecretRef string
	Source    string
	Redact    bool
	Value     string
	Success   bool
}

// InjectionEvent carries metadata for secret_injection events.
type InjectionEvent struct {
	InjectAs  string
	SecretRef string
	Source    string
	Success   bool
}

// Event returns metadata suitable for a secret_injection event.
func (i Injection) Event() InjectionEvent {
	return InjectionEvent{
		InjectAs:  i.InjectAs,
		SecretRef: i.SecretRef,
		Source:    i.Source,
		Success:   i.Success,
	}
}

// ResolveBindings resolves secret bindings against env and store values.
func ResolveBindings(bindings []Binding, store Store, env map[string]string) []Injection {
	resolved := make([]Injection, 0, len(bindings))
	for _, binding := range bindings {
		injection := Injection{
			InjectAs:  binding.InjectAs,
			SecretRef: binding.SecretRef,
			Source:    binding.Source,
		}
		if injection.Source == "" {
			injection.Source = defaultSource
		}
		injection.Redact = true
		if binding.Redact != nil {
			injection.Redact = *binding.Redact
		}

		switch injection.Source {
		case "env":
			if value, ok := lookupEnv(env, binding.SecretRef); ok {
				injection.Value = value
				injection.Success = true
			}
		case "file":
			if entry, ok := store[binding.SecretRef]; ok {
				if entry.Source == "" || entry.Source == injection.Source {
					if entry.Value != "" {
						injection.Value = entry.Value
						injection.Success = true
					}
				}
			}
		default:
			injection.Success = false
		}
		resolved = append(resolved, injection)
	}
	return resolved
}

// EnvMap converts os.Environ-style entries into a map.
func EnvMap(entries []string) map[string]string {
	env := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		env[parts[0]] = parts[1]
	}
	return env
}

func lookupEnv(env map[string]string, name string) (string, bool) {
	if value, ok := env[name]; ok && value != "" {
		return value, true
	}
	upper := strings.ToUpper(name)
	if value, ok := env[upper]; ok && value != "" {
		return value, true
	}
	if value, ok := env["SUB_SECRET_"+upper]; ok && value != "" {
		return value, true
	}
	return "", false
}
