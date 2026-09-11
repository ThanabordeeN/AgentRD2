package tools

import "github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"

// thinkSchema mirrors “THINK_SCHEMA“.
var thinkSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"style": map[string]any{
			"type":    "string",
			"enum":    []any{"neutral", "think", "ponder", "listen", "alert", "scheme"},
			"default": "think",
		},
		"duration": map[string]any{
			"type": "number", "minimum": 0.2, "maximum": 30.0, "default": 2.5,
		},
	},
	"additionalProperties": false,
}

// think mirrors “runtime/tools/think.py::think“. Both arguments have Python
// signature defaults ("think", 2.5), and the schema advertises the same
// values.
func think(ctx *Context, args map[string]any) domain.ToolResult {
	style, ok := stringArg(args, "style")
	if !ok {
		style = "think"
	}
	duration, ok := numberArg(args, "duration")
	if !ok {
		duration = 2.5
	}
	return ctx.DispatchAction("think", map[string]any{"style": style, "duration": duration})
}

// RegisterThinkTools registers the thinking family (1 tool).
func RegisterThinkTools(registry *Registry) {
	_ = registry.Register(Spec{
		Name: "think",
		Description: "Play a short thinking/listening/waiting gesture while a decision " +
			"or speech is being prepared.",
		Parameters: thinkSchema,
		Category:   "thinking",
		Handler:    think,
	})
}
