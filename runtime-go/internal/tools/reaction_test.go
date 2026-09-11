package tools

import "testing"

// TestReactionTools covers the reaction family's defaults and required
// arguments.
func TestReactionTools(t *testing.T) {
	runDispatchCases(t, []dispatchCase{
		{
			name:     "investigate forwards a position",
			tool:     "investigate",
			args:     map[string]any{"position": []any{1.0, 2.0, 3.0}},
			wantArgs: map[string]any{"position": []any{1.0, 2.0, 3.0}},
		},
		{
			name: "investigate fails without a position",
			tool: "investigate",
			args: map[string]any{},
		},
		{
			name: "investigate fails on an empty position",
			tool: "investigate",
			args: map[string]any{"position": []any{}},
		},
		{
			name:     "flee_from defaults entity to player",
			tool:     "flee_from",
			args:     map[string]any{},
			wantArgs: map[string]any{"entity": "player"},
		},
		{
			name:     "flee_from forwards an explicit entity",
			tool:     "flee_from",
			args:     map[string]any{"entity": "wolf_2"},
			wantArgs: map[string]any{"entity": "wolf_2"},
		},
		{
			name:     "wait defaults duration to the agent-level 2.0",
			tool:     "wait",
			args:     map[string]any{},
			wantArgs: map[string]any{"duration": 2.0},
		},
		{
			name:     "wait forwards an explicit duration",
			tool:     "wait",
			args:     map[string]any{"duration": 5},
			wantArgs: map[string]any{"duration": 5.0},
		},
		{
			name:     "wait falls back to 2.0 for a non-numeric duration",
			tool:     "wait",
			args:     map[string]any{"duration": "soon"},
			wantArgs: map[string]any{"duration": 2.0},
		},
		{
			name:     "react dispatches an empty payload when nothing is given",
			tool:     "react",
			args:     map[string]any{},
			wantArgs: map[string]any{},
		},
		{
			name: "react forwards entity, position, and reaction",
			tool: "react",
			args: map[string]any{
				"entity":   "player",
				"position": []any{4.0, 5.0, 6.0},
				"reaction": "startled",
			},
			wantArgs: map[string]any{
				"entity":   "player",
				"position": []any{4.0, 5.0, 6.0},
				"reaction": "startled",
			},
		},
		{
			name:     "hands_up defaults duration to 3.0",
			tool:     "hands_up",
			args:     map[string]any{},
			wantArgs: map[string]any{"duration": 3.0},
		},
		{
			name:     "hands_up forwards duration and face_entity",
			tool:     "hands_up",
			args:     map[string]any{"duration": 4, "face_entity": "player"},
			wantArgs: map[string]any{"duration": 4.0, "face_entity": "player"},
		},
		{
			name:     "cower defaults duration to 3.0",
			tool:     "cower",
			args:     map[string]any{},
			wantArgs: map[string]any{"duration": 3.0},
		},
		{
			name:     "cower forwards duration and from_entity",
			tool:     "cower",
			args:     map[string]any{"duration": 6, "from_entity": "bear_1"},
			wantArgs: map[string]any{"duration": 6.0, "from_entity": "bear_1"},
		},
		{
			name:     "duck defaults duration to 2.0",
			tool:     "duck",
			args:     map[string]any{},
			wantArgs: map[string]any{"duration": 2.0},
		},
		{
			name:     "duck forwards an explicit duration",
			tool:     "duck",
			args:     map[string]any{"duration": 1.5},
			wantArgs: map[string]any{"duration": 1.5},
		},
		{
			name:     "jump dispatches an empty payload",
			tool:     "jump",
			args:     map[string]any{},
			wantArgs: map[string]any{},
		},
		{
			name:     "walk_away forwards an entity",
			tool:     "walk_away",
			args:     map[string]any{"entity": "player"},
			wantArgs: map[string]any{"entity": "player"},
		},
		{
			name: "walk_away fails without an entity",
			tool: "walk_away",
			args: map[string]any{},
		},
	})
}
