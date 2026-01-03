package policy

import (
	"encoding/json"
)

func matchArgs(match *ArgsMatch, args map[string]any) bool {
	if match == nil || match.IsZero() {
		return true
	}
	if args == nil {
		return false
	}
	for _, key := range match.HasKeys {
		if _, ok := args[key]; !ok {
			return false
		}
	}
	for key, expected := range match.KeyEquals {
		if !valuesEqual(args[key], expected) {
			return false
		}
	}
	for key, allowed := range match.KeyIn {
		if !valueIn(args[key], allowed) {
			return false
		}
	}
	for key, numeric := range match.NumericRange {
		if !valueInRange(args[key], numeric) {
			return false
		}
	}
	return true
}

func valuesEqual(actual, expected any) bool {
	if actual == nil || expected == nil {
		return actual == expected
	}
	if aNum, ok := toFloat(actual); ok {
		if eNum, ok := toFloat(expected); ok {
			return aNum == eNum
		}
	}
	switch exp := expected.(type) {
	case string:
		act, ok := actual.(string)
		return ok && act == exp
	case bool:
		act, ok := actual.(bool)
		return ok && act == exp
	default:
		return actual == expected
	}
}

func valueIn(actual any, allowed []any) bool {
	for _, candidate := range allowed {
		if valuesEqual(actual, candidate) {
			return true
		}
	}
	return false
}

func valueInRange(actual any, numeric NumericRange) bool {
	value, ok := toFloat(actual)
	if !ok {
		return false
	}
	if numeric.Min != nil && value < *numeric.Min {
		return false
	}
	if numeric.Max != nil && value > *numeric.Max {
		return false
	}
	return true
}

func toFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return f, true
		}
		return 0, false
	default:
		return 0, false
	}
}
