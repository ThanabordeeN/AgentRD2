package domain

import (
	"encoding/json"
	"strconv"
)

// The runtime exchanges JSON with the bridge and the timeline. Python is
// permissive about numeric types (``int(value.get("day", 0))``), so these
// helpers mirror that tolerance instead of failing on a float where an int is
// expected.

// IntFrom coerces a decoded JSON value to int.
func IntFrom(value any) int {
	switch typed := value.(type) {
	case nil:
		return 0
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
		if parsed, err := typed.Float64(); err == nil {
			return int(parsed)
		}
	case bool:
		if typed {
			return 1
		}
		return 0
	case string:
		if parsed, err := strconv.ParseFloat(typed, 64); err == nil {
			return int(parsed)
		}
	}
	return 0
}

// FloatFrom coerces a decoded JSON value to float64.
func FloatFrom(value any) float64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseFloat(typed, 64); err == nil {
			return parsed
		}
	}
	return 0
}

// StringFrom coerces a decoded JSON value to string.
func StringFrom(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	}
	return ""
}

// StringsFrom coerces a decoded JSON array to a []string.
func StringsFrom(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			return typed
		}
		return []string{}
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		out = append(out, StringFrom(item))
	}
	return out
}

// BoolPtrFrom coerces a decoded JSON value to *bool, preserving the
// distinction between "false" and "the bridge did not say". The eligibility
// gate fails closed on the latter.
func BoolPtrFrom(value any) *bool {
	switch typed := value.(type) {
	case nil:
		return nil
	case bool:
		return &typed
	case string:
		switch typed {
		case "true", "True", "1", "yes":
			result := true
			return &result
		case "false", "False", "0", "no":
			result := false
			return &result
		}
	case float64:
		result := typed != 0
		return &result
	case int:
		result := typed != 0
		return &result
	}
	return nil
}

// FloatPtrFrom coerces a decoded JSON value to *float64, preserving "absent".
func FloatPtrFrom(value any) *float64 {
	if value == nil {
		return nil
	}
	result := FloatFrom(value)
	return &result
}

// MapFrom returns a decoded JSON object, or an empty map.
func MapFrom(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

// MapsFrom coerces a decoded JSON array of objects to []map[string]any.
func MapsFrom(value any) []map[string]any {
	raw, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		out = append(out, MapFrom(item))
	}
	return out
}
