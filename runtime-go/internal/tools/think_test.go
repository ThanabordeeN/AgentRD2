package tools

import "testing"

// TestThinkTools covers the think template defaults from the Python signature.
func TestThinkTools(t *testing.T) {
	runDispatchCases(t, []dispatchCase{
		{
			name:     "think applies the style and duration defaults",
			tool:     "think",
			args:     map[string]any{},
			wantArgs: map[string]any{"style": "think", "duration": 2.5},
		},
		{
			name:     "think forwards explicit style and duration",
			tool:     "think",
			args:     map[string]any{"style": "scheme", "duration": 5},
			wantArgs: map[string]any{"style": "scheme", "duration": 5.0},
		},
		{
			name:     "think defaults only the missing argument",
			tool:     "think",
			args:     map[string]any{"style": "alert"},
			wantArgs: map[string]any{"style": "alert", "duration": 2.5},
		},
		{
			name:     "think treats a nil argument as absent",
			tool:     "think",
			args:     map[string]any{"style": nil, "duration": nil},
			wantArgs: map[string]any{"style": "think", "duration": 2.5},
		},
		{
			name:     "think passes an out-of-enum style through like Python",
			tool:     "think",
			args:     map[string]any{"style": "brood", "duration": 1.0},
			wantArgs: map[string]any{"style": "brood", "duration": 1.0},
		},
	})
}
