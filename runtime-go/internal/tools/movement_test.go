package tools

import "testing"

// TestMovementTools covers the movement family's defaulting and dispatch
// behaviour, mirroring runtime/tools/movement.py plus the agent-level radius
// default from AgentRuntime._normalize_action.
func TestMovementTools(t *testing.T) {
	runDispatchCases(t, []dispatchCase{
		{
			name:     "wander defaults radius to the agent-level 8.0",
			tool:     "wander",
			args:     map[string]any{},
			wantArgs: map[string]any{"radius": 8.0},
		},
		{
			name:     "wander forwards an explicit radius",
			tool:     "wander",
			args:     map[string]any{"radius": 12.5},
			wantArgs: map[string]any{"radius": 12.5},
		},
		{
			name:     "wander coerces a numeric string",
			tool:     "wander",
			args:     map[string]any{"radius": "9"},
			wantArgs: map[string]any{"radius": 9.0},
		},
		{
			name:     "wander falls back to 8.0 for a non-numeric radius",
			tool:     "wander",
			args:     map[string]any{"radius": "far"},
			wantArgs: map[string]any{"radius": 8.0},
		},
		{
			name:     "go_to forwards a named destination",
			tool:     "go_to",
			args:     map[string]any{"destination": "Valentine"},
			wantArgs: map[string]any{"destination": "Valentine"},
		},
		{
			name: "go_to forwards a positional destination and priority",
			tool: "go_to",
			args: map[string]any{
				"destination": map[string]any{"region": "Lemoyne", "position": []any{1.0, 2.0, 3.0}},
				"priority":    "high",
			},
			wantArgs: map[string]any{
				"destination": map[string]any{"region": "Lemoyne", "position": []any{1.0, 2.0, 3.0}},
				"priority":    "high",
			},
		},
		{
			name: "go_to fails without a destination",
			tool: "go_to",
			args: map[string]any{"priority": "high"},
		},
		{
			name:     "follow defaults entity to player",
			tool:     "follow",
			args:     map[string]any{},
			wantArgs: map[string]any{"entity": "player"},
		},
		{
			name:     "follow forwards entity and distance",
			tool:     "follow",
			args:     map[string]any{"entity": "Dutch", "distance": 4.5},
			wantArgs: map[string]any{"entity": "Dutch", "distance": 4.5},
		},
		{
			name:     "follow omits an absent distance",
			tool:     "follow",
			args:     map[string]any{"entity": "Dutch"},
			wantArgs: map[string]any{"entity": "Dutch"},
		},
		{
			name:     "stop dispatches an empty payload",
			tool:     "stop",
			args:     map[string]any{},
			wantArgs: map[string]any{},
		},
	})
}
