package tools

import (
	"strings"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

// saySchema mirrors “SAY_SCHEMA“.
var saySchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"text":    map[string]any{"type": "string", "minLength": 1, "maxLength": 500},
		"target":  map[string]any{"type": "string"},
		"emotion": map[string]any{"type": "string"},
	},
	"required":             []any{"text"},
	"additionalProperties": false,
}

// gestureSchema mirrors “GESTURE_SCHEMA“.
var gestureSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"type": map[string]any{
			"type": "string",
			"enum": []any{
				"neutral", "friendly", "annoyed", "afraid",
				"wave", "nod", "shrug", "think", "ponder", "scheme",
			},
		},
	},
	"required":             []any{"type"},
	"additionalProperties": false,
}

// say mirrors “runtime/tools/speech.py::say“. “text“ is required and, as in
// the Python agent's “say_missing_text“ guard, must not be blank; “target“
// and “emotion“ are only forwarded when supplied.
func say(ctx *Context, args map[string]any) domain.ToolResult {
	text, ok := stringArg(args, "text")
	if !ok || strings.TrimSpace(text) == "" {
		return failedCall("say", ctx, args, "missing required argument: text")
	}
	out := map[string]any{"text": text}
	if target, ok := stringArg(args, "target"); ok {
		out["target"] = target
	}
	if emotion, ok := stringArg(args, "emotion"); ok {
		out["emotion"] = emotion
	}
	return ctx.DispatchAction("say", out)
}

// gesture mirrors “gesture“. The agent layer defaults a missing type to
// "neutral"; the enum itself is only enforced by the JSON schema.
func gesture(ctx *Context, args map[string]any) domain.ToolResult {
	gestureType, ok := stringArg(args, "type")
	if !ok {
		gestureType = "neutral"
	}
	return ctx.DispatchAction("gesture", map[string]any{"type": gestureType})
}

// RegisterSpeechTools registers the speech family (2 tools).
func RegisterSpeechTools(registry *Registry) {
	_ = registry.Register(Spec{
		Name:        "say",
		Description: "Speak a short line through the TTS pipeline.",
		Parameters:  saySchema,
		Category:    "speech",
		Handler:     say,
	})
	_ = registry.Register(Spec{
		Name:        "gesture",
		Description: "Play a generic gesture animation.",
		Parameters:  gestureSchema,
		Category:    "speech",
		Handler:     gesture,
	})
}
