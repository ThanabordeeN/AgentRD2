package tools

import "testing"

// TestAttentionTools covers the attention family, including the agent-level
// entity default.
func TestAttentionTools(t *testing.T) {
	runDispatchCases(t, []dispatchCase{
		{
			name:     "look_at defaults entity to player",
			tool:     "look_at",
			args:     map[string]any{},
			wantArgs: map[string]any{"entity": "player"},
		},
		{
			name:     "look_at forwards an explicit entity",
			tool:     "look_at",
			args:     map[string]any{"entity": "ped_7"},
			wantArgs: map[string]any{"entity": "ped_7"},
		},
		{
			name:     "look_at keeps an explicit empty entity",
			tool:     "look_at",
			args:     map[string]any{"entity": ""},
			wantArgs: map[string]any{"entity": ""},
		},
		{
			name:     "face defaults entity to player",
			tool:     "face",
			args:     map[string]any{},
			wantArgs: map[string]any{"entity": "player"},
		},
		{
			name:     "face forwards an explicit entity",
			tool:     "face",
			args:     map[string]any{"entity": "horse_1"},
			wantArgs: map[string]any{"entity": "horse_1"},
		},
		{
			name:     "clear_attention dispatches an empty payload",
			tool:     "clear_attention",
			args:     map[string]any{},
			wantArgs: map[string]any{},
		},
	})
}
