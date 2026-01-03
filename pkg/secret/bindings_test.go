package secret

import "testing"

func TestParseBindingsErrorsOnInvalidEntry(t *testing.T) {
	raw := `[
  {
    "server_name": "server-1",
    "secret_bindings": [
      { "inject_as": "API_KEY" }
    ]
  }
]`

	if _, err := ParseBindings([]byte(raw), "server-1"); err == nil {
		t.Fatalf("expected error for invalid binding, got nil")
	}
}
