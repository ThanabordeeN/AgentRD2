// Package domain holds the canonical data structures shared by the runtime.
//
// It is a direct port of the Python ``runtime/schemas.py`` module, and the
// JSON shape of every type is intentionally identical: the timeline is a
// shared, append-only JSONL store and the IPC protocol is a shared contract,
// so both implementations must be able to read each other's output.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"math"
	"strconv"
	"time"
)

// Timestamp returns a Unix timestamp with sub-second precision, matching
// Python's ``time.time()``.
func Timestamp() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// NewEventID returns an identifier in the same shape as the Python runtime
// (``evt_`` followed by 12 hex characters).
func NewEventID() string {
	return "evt_" + RandomHex(12)
}

// NewActionID returns an ``act_``-prefixed identifier.
func NewActionID() string {
	return "act_" + RandomHex(12)
}

// RandomHex returns n random hex characters.
func RandomHex(n int) string {
	if n <= 0 {
		return ""
	}
	buf := make([]byte, (n+1)/2)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand should never fail; fall back to a time-derived value so
		// a ped still gets a usable identifier instead of an empty string.
		seed := strconv.FormatInt(time.Now().UnixNano(), 16)
		for len(seed) < n {
			seed += seed
		}
		return seed[:n]
	}
	return hex.EncodeToString(buf)[:n]
}

// CleanNone recursively drops nil values from JSON-like structures, matching
// the Python helper of the same name. Python omits ``None`` keys entirely
// rather than emitting ``null``, and the timeline files are shared.
func CleanNone(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if item == nil {
				continue
			}
			cleaned := CleanNone(item)
			if cleaned == nil {
				continue
			}
			out[key] = cleaned
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, CleanNone(item))
		}
		return out
	default:
		return value
	}
}

// CleanMap is CleanNone specialised for the string-keyed maps the runtime
// passes around as event ``data``.
func CleanMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	cleaned, _ := CleanNone(value).(map[string]any)
	if cleaned == nil {
		return map[string]any{}
	}
	return cleaned
}

// ClampImportance bounds an importance score to [0, 1], mirroring
// ``Event.__post_init__``.
func ClampImportance(value *float64) *float64 {
	if value == nil {
		return nil
	}
	clamped := math.Max(0, math.Min(1, *value))
	return &clamped
}
