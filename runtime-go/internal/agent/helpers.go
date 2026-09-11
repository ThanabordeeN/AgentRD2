package agent

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// coerceFloat mirrors Python's ``float(value)`` for the JSON-shaped values the
// runtime handles: numbers, numeric strings, and booleans convert; anything
// else reports false so callers can apply the Python default.
func coerceFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case bool:
		if typed {
			return 1, true
		}
		return 0, true
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

// isTruthy mirrors Python truthiness for the JSON-shaped values used in
// decisions: nil, false, zero, and empty strings/collections are falsy.
func isTruthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case float64:
		return typed != 0
	case float32:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case []any:
		return len(typed) > 0
	case []string:
		return len(typed) > 0
	case []float64:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	}
	return true
}

// pyStr mirrors Python's ``str(value)`` for the scalar JSON values that reach
// the runtime's error messages.
func pyStr(value any) string {
	switch typed := value.(type) {
	case nil:
		return "None"
	case string:
		return typed
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case float64:
		return pythonFloat(typed)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		if text, ok := value.(interface{ String() string }); ok {
			return text.String()
		}
		return fmt.Sprintf("%v", value)
	}
}

// pyReprString mirrors Python's ``repr`` for the short strings that appear in
// error messages (single quotes unless the value contains one and no double
// quote).
func pyReprString(value string) string {
	quote := byte('\'')
	if strings.ContainsRune(value, '\'') && !strings.ContainsRune(value, '"') {
		quote = '"'
	}
	var builder strings.Builder
	builder.WriteByte(quote)
	for _, r := range value {
		switch {
		case r == rune(quote) || r == '\\':
			builder.WriteByte('\\')
			builder.WriteRune(r)
		case r == '\n':
			builder.WriteString(`\n`)
		case r == '\r':
			builder.WriteString(`\r`)
		case r == '\t':
			builder.WriteString(`\t`)
		case unicode.IsPrint(r):
			builder.WriteRune(r)
		case r <= 0xFF:
			builder.WriteString(`\x`)
			builder.WriteString(strings.ToLower(strconv.FormatInt(int64(r), 16)))
		case r <= 0xFFFF:
			builder.WriteString(`\u`)
			builder.WriteString(strings.ToLower(strconv.FormatInt(int64(r), 16)))
		default:
			builder.WriteString(`\U`)
			builder.WriteString(strings.ToLower(strconv.FormatInt(int64(r), 16)))
		}
	}
	builder.WriteByte(quote)
	return builder.String()
}
