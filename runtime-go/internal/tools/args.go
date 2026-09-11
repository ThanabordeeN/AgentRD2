package tools

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

// Conversion helpers shared by the tool handlers. The Python runtime invokes
// handlers with keyword arguments, so a handler never sees ``None`` (the agent
// drops such arguments before the call) and a missing required argument raises
// ``TypeError``, which ``_call_tool`` converts into a failed result. These
// helpers reproduce both behaviours: nil means absent, and a missing required
// argument produces a failed ``domain.ToolResult``.

// provided reports whether the caller supplied a usable (non-nil) value.
// Python's “_call_tool“ drops “None“ arguments, so nil and absent are
// indistinguishable.
func provided(args map[string]any, key string) bool {
	value, ok := args[key]
	return ok && value != nil
}

// raw returns the unconverted value for key when it was provided.
func raw(args map[string]any, key string) (any, bool) {
	value, ok := args[key]
	if !ok || value == nil {
		return nil, false
	}
	return value, true
}

// stringArg coerces a provided argument to string.
func stringArg(args map[string]any, key string) (string, bool) {
	value, ok := raw(args, key)
	if !ok {
		return "", false
	}
	return domain.StringFrom(value), true
}

// numberArg coerces a provided argument to float64, reporting false when the
// value is absent or not numeric. Python's “float(...)“/“int(...)“ casts
// raise in that case, and the runtime turns the exception into a failed
// ToolResult.
func numberArg(args map[string]any, key string) (float64, bool) {
	value, ok := raw(args, key)
	if !ok {
		return 0, false
	}
	return numberValue(value)
}

// intArg coerces a provided argument to int with Python “int(...)“
// semantics: floats truncate toward zero and bools become 0/1.
func intArg(args map[string]any, key string) (int, bool) {
	value, ok := numberArg(args, key)
	if !ok {
		return 0, false
	}
	return int(value), true
}

// numberValue coerces one decoded JSON value to float64.
func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case bool:
		if typed {
			return 1, true
		}
		return 0, true
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

// positionArg returns a position argument that Python's “or“ guards treat as
// present: a non-empty list. Used by “investigate“, whose agent-level
// fallback (the NPC's most recent known position) is not reproducible here.
func positionArg(args map[string]any, key string) (any, bool) {
	value, ok := raw(args, key)
	if !ok || emptyList(value) {
		return nil, false
	}
	return value, true
}

// emptyList reports whether a decoded JSON value is an empty array.
func emptyList(value any) bool {
	switch typed := value.(type) {
	case []any:
		return len(typed) == 0
	case []float64:
		return len(typed) == 0
	case []string:
		return len(typed) == 0
	}
	return false
}

// requestFor builds the request the Python “_call_tool“ wrapper attaches to
// a result, including the generated request id and the NPC id.
func requestFor(tool string, ctx *Context, args map[string]any) domain.ActionRequest {
	var npcID string
	if ctx != nil {
		npcID = ctx.NPCID
	}
	return domain.NewActionRequest(tool, args, npcID)
}

// failedCall mirrors the “ToolResult.failed(request, reason=...)“ path of
// Python's “_call_tool“.
func failedCall(tool string, ctx *Context, args map[string]any, reason string) domain.ToolResult {
	return domain.FailedResult(requestFor(tool, ctx, args), reason, nil)
}

// completedData mirrors the perception path of “_call_tool“:
// “ToolResult.completed(request, result=payload)“. Perception tools return
// data rather than bridge actions, so the payload is nested under “result“.
func completedData(tool string, ctx *Context, args map[string]any, payload any) domain.ToolResult {
	return domain.CompletedResult(requestFor(tool, ctx, args), map[string]any{"result": payload})
}

// worldOf returns the world reader, tolerating a nil context.
func worldOf(ctx *Context) WorldReader {
	if ctx == nil {
		return nil
	}
	return ctx.World
}

// loreOf returns the lore reader, tolerating a nil context.
func loreOf(ctx *Context) LoreReader {
	if ctx == nil {
		return nil
	}
	return ctx.Lore
}

// profileOf returns the NPC profile, tolerating a nil context. A nil profile
// behaves like Python's “context.profile or {}“.
func profileOf(ctx *Context) map[string]any {
	if ctx == nil || ctx.Profile == nil {
		return map[string]any{}
	}
	return ctx.Profile
}

// eventMaps mirrors “[event.to_dict() for event in events]“.
func eventMaps(events []domain.Event) []map[string]any {
	out := make([]map[string]any, 0, len(events))
	for _, event := range events {
		out = append(out, event.ToMap())
	}
	return out
}

// pageMaps normalises a lore result so it always marshals as a JSON array.
func pageMaps(pages []map[string]any) []map[string]any {
	if pages == nil {
		return []map[string]any{}
	}
	return pages
}
