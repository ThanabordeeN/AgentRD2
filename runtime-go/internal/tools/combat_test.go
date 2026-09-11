package tools

import "testing"

// TestCombatTools covers the deferred combat family. Note that shoot_at's
// Python signature defaults duration to 0.5 even though TARGET_SCHEMA
// advertises 3.0.
func TestCombatTools(t *testing.T) {
	runDispatchCases(t, []dispatchCase{
		{
			name:     "aim_at defaults duration to 3.0",
			tool:     "aim_at",
			args:     map[string]any{},
			wantArgs: map[string]any{"duration": 3.0},
		},
		{
			name:     "aim_at forwards entity, position, and duration",
			tool:     "aim_at",
			args:     map[string]any{"entity": "player", "position": []any{1.0, 2.0, 3.0}, "duration": 2},
			wantArgs: map[string]any{"entity": "player", "position": []any{1.0, 2.0, 3.0}, "duration": 2.0},
		},
		{
			name:     "shoot_at defaults duration to the signature value 0.5",
			tool:     "shoot_at",
			args:     map[string]any{},
			wantArgs: map[string]any{"duration": 0.5},
		},
		{
			name:     "shoot_at forwards an explicit duration",
			tool:     "shoot_at",
			args:     map[string]any{"duration": 3.0},
			wantArgs: map[string]any{"duration": 3.0},
		},
		{
			name:     "shoot_at forwards an entity",
			tool:     "shoot_at",
			args:     map[string]any{"entity": "player"},
			wantArgs: map[string]any{"duration": 0.5, "entity": "player"},
		},
		{
			name:     "attack forwards an entity",
			tool:     "attack",
			args:     map[string]any{"entity": "player"},
			wantArgs: map[string]any{"entity": "player"},
		},
		{
			name: "attack fails without an entity",
			tool: "attack",
			args: map[string]any{},
		},
		{
			name:     "take_cover defaults duration to 5.0",
			tool:     "take_cover",
			args:     map[string]any{},
			wantArgs: map[string]any{"duration": 5.0},
		},
		{
			name: "take_cover forwards from_entity and from_position",
			tool: "take_cover",
			args: map[string]any{
				"from_entity":   "player",
				"from_position": []any{7.0, 8.0, 9.0},
				"duration":      10,
			},
			wantArgs: map[string]any{
				"from_entity":   "player",
				"from_position": []any{7.0, 8.0, 9.0},
				"duration":      10.0,
			},
		},
	})
}
