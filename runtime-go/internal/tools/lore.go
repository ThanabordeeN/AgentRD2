package tools

import "github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"

// getWorldLoreSchema mirrors “GET_WORLD_LORE_SCHEMA“.
var getWorldLoreSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"topic": map[string]any{
			"type":        "string",
			"description": "Location, faction, activity, or event to look up.",
		},
	},
	"additionalProperties": false,
}

// Lore lookup defaults, matching the Python store's keyword arguments:
// “WikiContextStore.lookup(query, limit=3, max_chars=1200)“ and
// “context_for_profile(profile, max_topics=5, max_chars_per_topic=1200)“.
const (
	loreLookupLimit     = 3
	loreLookupMaxChars  = 1200
	loreProfileTopics   = 5
	loreProfileMaxChars = 1200
)

// getWorldLore mirrors “runtime/tools/lore.py::get_world_lore“. A missing
// lore store is not an error: Python returns “{"lore": []}“. With no topic,
// the NPC profile drives the lookup.
func getWorldLore(ctx *Context, args map[string]any) domain.ToolResult {
	lore := loreOf(ctx)
	if lore == nil {
		return completedData("get_world_lore", ctx, args, map[string]any{"lore": []map[string]any{}})
	}
	if topic, ok := stringArg(args, "topic"); ok && topic != "" {
		pages := lore.Lookup(topic, loreLookupLimit, loreLookupMaxChars)
		return completedData("get_world_lore", ctx, args, map[string]any{"lore": pageMaps(pages)})
	}
	pages := lore.ContextForProfile(profileOf(ctx), loreProfileTopics, loreProfileMaxChars)
	return completedData("get_world_lore", ctx, args, map[string]any{"lore": pageMaps(pages)})
}

// RegisterLoreTools registers the perception tool “get_world_lore“.
func RegisterLoreTools(registry *Registry) {
	_ = registry.Register(Spec{
		Name:        "get_world_lore",
		Description: "Look up offline Red Dead Wiki lore/context for a topic or for this NPC.",
		Parameters:  getWorldLoreSchema,
		Category:    "perception",
		Handler:     getWorldLore,
	})
}
