// Package core provides protocol-agnostic enforcement core functionality.
//
// This file handles identity generation and environment variable reading
// for the shim. Identity values are stamped on every event.
//
// Per Interface-Pack §0.3 and §5:
// - run_id MUST be globally unique per run
// - Format is arbitrary but SHOULD be ULID/UUIDv7-style (UUID v4 is acceptable)
// - Identity comes from SUB_* environment variables
package core

import (
	"crypto/rand"
	"fmt"
	"os"

	"github.com/subluminal/subluminal/pkg/event"
)

// Identity contains the identity values for a shim instance.
type Identity struct {
	RunID   string       // Globally unique run identifier
	AgentID string       // Agent identifier (from SUB_AGENT_ID or default)
	Client  event.Client // Client type (from SUB_CLIENT or default)
	Env     event.Env    // Environment (from SUB_ENV or default)
}

// Source contains the producer instance identifiers.
// These are generated fresh for each shim process.
type Source struct {
	HostID string // Stable per-machine ID (generated once per machine ideally, but UUID per run is OK for v0.1)
	ProcID string // Stable per-process ID
	ShimID string // Unique per shim instance
}

// GenerateUUID generates a UUID v4 using crypto/rand.
// Returns lowercase hex string with dashes: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
func GenerateUUID() string {
	uuid := make([]byte, 16)
	_, err := rand.Read(uuid)
	if err != nil {
		// Fallback to a predictable but unique-ish ID
		// This should never happen in practice
		return "00000000-0000-4000-8000-000000000000"
	}

	// Set version (4) and variant (RFC 4122)
	uuid[6] = (uuid[6] & 0x0f) | 0x40 // Version 4
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // Variant RFC 4122

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4],
		uuid[4:6],
		uuid[6:8],
		uuid[8:10],
		uuid[10:16])
}

// ReadIdentityFromEnv reads identity values from environment variables.
// Falls back to defaults if not set.
//
// Environment variables (per Interface-Pack §5):
//   - SUB_RUN_ID: Globally unique run ID (generated if not set)
//   - SUB_AGENT_ID: Agent identifier (defaults to "unknown")
//   - SUB_CLIENT: Client type - "claude" | "codex" | "headless" | "custom" | "unknown"
//   - SUB_ENV: Environment - "dev" | "ci" | "prod" | "unknown"
func ReadIdentityFromEnv() Identity {
	id := Identity{
		RunID:   os.Getenv("SUB_RUN_ID"),
		AgentID: os.Getenv("SUB_AGENT_ID"),
		Client:  parseClient(os.Getenv("SUB_CLIENT")),
		Env:     parseEnv(os.Getenv("SUB_ENV")),
	}

	// Generate run_id if not provided
	if id.RunID == "" {
		id.RunID = GenerateUUID()
	}

	// Default agent_id
	if id.AgentID == "" {
		id.AgentID = "unknown"
	}

	return id
}

// GenerateSource creates a new Source with fresh UUIDs.
// Called once per shim startup.
func GenerateSource() Source {
	return Source{
		HostID: GenerateUUID(), // Ideally stable per machine, but UUID per run is OK for v0.1
		ProcID: GenerateUUID(), // Stable per process
		ShimID: GenerateUUID(), // Unique per shim instance
	}
}

// ToEventSource converts Source to event.Source for embedding in events.
func (s Source) ToEventSource() event.Source {
	return event.Source{
		HostID: s.HostID,
		ProcID: s.ProcID,
		ShimID: s.ShimID,
	}
}

// parseClient converts a string to event.Client.
func parseClient(s string) event.Client {
	switch s {
	case "claude":
		return event.ClientClaude
	case "codex":
		return event.ClientCodex
	case "headless":
		return event.ClientHeadless
	case "custom":
		return event.ClientCustom
	default:
		return event.ClientUnknown
	}
}

// parseEnv converts a string to event.Env.
func parseEnv(s string) event.Env {
	switch s {
	case "dev":
		return event.EnvDev
	case "ci":
		return event.EnvCI
	case "prod":
		return event.EnvProd
	default:
		return event.EnvUnknown
	}
}

// InterfaceVersion is the Interface-Pack version this implementation targets.
const InterfaceVersion = "0.1.0"
