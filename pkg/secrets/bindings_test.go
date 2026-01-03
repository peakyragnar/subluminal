package secrets

import (
	"reflect"
	"testing"
)

func TestParseSecretBindingsArray(t *testing.T) {
	raw := []any{
		map[string]any{
			"inject_as":  "API_TOKEN",
			"secret_ref": "github_token",
			"source":     "env",
			"redact":     false,
		},
		map[string]any{
			"inject_as":  "OTHER_TOKEN",
			"secret_ref": "other_token",
		},
	}

	got, err := ParseSecretBindings(raw)
	if err != nil {
		t.Fatalf("ParseSecretBindings error: %v", err)
	}

	want := SecretBindings{
		"API_TOKEN": {
			SecretRef: "github_token",
			Source:    SecretSourceEnv,
			Redact:    false,
		},
		"OTHER_TOKEN": {
			SecretRef: "other_token",
			Source:    SecretSourceEnv,
			Redact:    true,
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected bindings\n got: %#v\nwant: %#v", got, want)
	}
}

func TestParseSecretBindingsMap(t *testing.T) {
	raw := map[string]any{
		"API_TOKEN": "github_token",
	}

	got, err := ParseSecretBindings(raw)
	if err != nil {
		t.Fatalf("ParseSecretBindings error: %v", err)
	}

	want := SecretBindings{
		"API_TOKEN": {
			SecretRef: "github_token",
			Source:    SecretSourceEnv,
			Redact:    true,
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected bindings\n got: %#v\nwant: %#v", got, want)
	}
}

func TestParseSecretBindingsErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  any
	}{
		{
			name: "invalid type",
			raw:  "not-valid",
		},
		{
			name: "non-object entry",
			raw:  []any{"bad"},
		},
		{
			name: "missing inject_as",
			raw: []any{
				map[string]any{
					"secret_ref": "github_token",
				},
			},
		},
		{
			name: "missing secret_ref",
			raw: []any{
				map[string]any{
					"inject_as": "API_TOKEN",
				},
			},
		},
		{
			name: "invalid source",
			raw: []any{
				map[string]any{
					"inject_as":  "API_TOKEN",
					"secret_ref": "github_token",
					"source":     "nope",
				},
			},
		},
		{
			name: "duplicate inject_as",
			raw: []any{
				map[string]any{
					"inject_as":  "API_TOKEN",
					"secret_ref": "one",
				},
				map[string]any{
					"inject_as":  "API_TOKEN",
					"secret_ref": "two",
				},
			},
		},
		{
			name: "map value not string",
			raw: map[string]any{
				"API_TOKEN": 123,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSecretBindings(tc.raw)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestEnvInjectionMap(t *testing.T) {
	bindings := SecretBindings{
		"API_TOKEN": {
			SecretRef: "github_token",
			Source:    SecretSourceEnv,
			Redact:    true,
		},
	}

	got, err := EnvInjectionMap(bindings)
	if err != nil {
		t.Fatalf("EnvInjectionMap error: %v", err)
	}

	want := map[string]string{
		"API_TOKEN": "github_token",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected env map\n got: %#v\nwant: %#v", got, want)
	}

	bindings["BAD_TOKEN"] = SecretBinding{
		SecretRef: "oops",
		Source:    SecretSourceKeychain,
		Redact:    true,
	}

	if _, err := EnvInjectionMap(bindings); err == nil {
		t.Fatalf("expected error for unsupported source")
	}
}
