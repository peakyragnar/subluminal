package event

// serialize.go - JSONL serialization for events
//
// PURPOSE IN SUBLUMINAL:
// Events must be streamed to multiple destinations (stdout, ledger, files, collectors).
// JSONL (JSON Lines) is the wire format: one compact JSON object per line, newline-terminated.
// This makes events trivially parseable, streamable, and grep-able.
//
// FLOW:
//   Event struct (ToolCallStartEvent, etc.)
//       ↓
//   SerializeEvent()
//       ↓
//   []byte: {"v":"0.1.0","type":"tool_call_start",...}\n
//       ↓
//   stdout | SQLite | file | network
//
// CONTRACT (Interface-Pack.md §1.1):
// - One JSON object per line (no pretty-printing)
// - UTF-8 encoding
// - Newline \n terminator (not \r\n)
// - NO multi-line JSON objects

import "encoding/json"

// SerializeEvent converts an event to JSONL format (single line + newline).
// Per Interface-Pack §1.1:
// - One JSON object per line
// - UTF-8 encoding
// - Newline \n terminator
// - NO multi-line JSON objects
//
// Uses json.Marshal which produces compact JSON (no whitespace/newlines).
func SerializeEvent(event any) ([]byte, error) {
	// json.Marshal produces compact JSON (single line, no indentation)
	jsonBytes, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	// Append newline terminator per JSONL spec
	jsonBytes = append(jsonBytes, '\n')

	return jsonBytes, nil
}
