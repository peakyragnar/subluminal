// Package canonical provides JSON canonicalization and hashing per Interface-Pack §1.9.1.
//
// PURPOSE IN SUBLUMINAL:
// This package creates a unique fingerprint (args_hash) for every tool call an agent makes.
// The fingerprint is used throughout Subluminal for:
//   - Dedupe:          "Agent already made this exact call" → block duplicate
//   - Policy matching: "Block calls with these specific arguments"
//   - Ledger/Audit:    "Show me all calls with this fingerprint"
//   - Breaker:         "Agent made the same call 10 times" → trip breaker
//
// Every tool_call_start event includes args_hash, which comes from this package.
//
// WHY CANONICALIZATION:
// Without canonicalization, {"a":1,"b":2} and {"b":2,"a":1} would produce different
// hashes even though they represent the same data. Canonicalization ensures:
//
//	Same data → Same canonical form → Same hash (always)
//
// CONTRACT REQUIREMENTS (Interface-Pack.md §1.9.1):
// - UTF-8 encoding
// - Objects: keys sorted lexicographically by Unicode codepoint
// - No insignificant whitespace
// - Numbers in minimal decimal form without trailing zeros
// - Arrays retain order
// - Standard JSON escaping
// - args_hash = SHA-256(canonical_args_bytes) lowercase hex
package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Canonicalize converts a JSON-compatible value to canonical JSON bytes.
// Per Interface-Pack §1.9.1:
// - Object keys sorted lexicographically by Unicode codepoint
// - No insignificant whitespace
// - Numbers in minimal decimal form
// - Arrays retain order
func Canonicalize(v any) ([]byte, error) {
	return canonicalizeValue(v)
}

// ArgsHash computes the SHA-256 hash of the canonical JSON representation.
// Returns lowercase hex string.
// Per Interface-Pack §1.9.1: args_hash = SHA-256(canonical_args_bytes)
func ArgsHash(v any) (string, error) {
	canonicalBytes, err := Canonicalize(v)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(canonicalBytes)
	return hex.EncodeToString(hash[:]), nil
}

// canonicalizeValue recursively converts a value to canonical JSON bytes.
func canonicalizeValue(v any) ([]byte, error) {
	switch val := v.(type) {
	case nil:
		return []byte("null"), nil

	case bool:
		if val {
			return []byte("true"), nil
		}
		return []byte("false"), nil

	case string:
		// Use json.Marshal for proper string escaping
		return json.Marshal(val)

	case float64:
		// Use json.Marshal for minimal decimal form
		return json.Marshal(val)

	case int:
		return json.Marshal(val)

	case int64:
		return json.Marshal(val)

	case json.Number:
		return []byte(val.String()), nil

	case []any:
		return canonicalizeArray(val)

	case map[string]any:
		return canonicalizeObject(val)

	default:
		return nil, fmt.Errorf("unsupported type: %T", v)
	}
}

// canonicalizeArray converts an array to canonical JSON bytes.
// Arrays retain their order (not sorted).
func canonicalizeArray(arr []any) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('[')

	for i, elem := range arr {
		if i > 0 {
			buf.WriteByte(',')
		}
		elemBytes, err := canonicalizeValue(elem)
		if err != nil {
			return nil, err
		}
		buf.Write(elemBytes)
	}

	buf.WriteByte(']')
	return buf.Bytes(), nil
}

// canonicalizeObject converts an object to canonical JSON bytes.
// Keys are sorted lexicographically by Unicode codepoint.
func canonicalizeObject(obj map[string]any) ([]byte, error) {
	// Extract and sort keys lexicographically
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys) // Lexicographic sort by Unicode codepoint

	var buf bytes.Buffer
	buf.WriteByte('{')

	for i, key := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}

		// Write key (with proper JSON escaping)
		keyBytes, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buf.Write(keyBytes)

		buf.WriteByte(':')

		// Write value (recursively canonicalized)
		valBytes, err := canonicalizeValue(obj[key])
		if err != nil {
			return nil, err
		}
		buf.Write(valBytes)
	}

	buf.WriteByte('}')
	return buf.Bytes(), nil
}
