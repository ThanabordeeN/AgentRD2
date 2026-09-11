package tools

import "testing"

// TestSpeechTools covers say/gesture, including the agent-level gesture type
// default and the blank-text guard.
func TestSpeechTools(t *testing.T) {
	runDispatchCases(t, []dispatchCase{
		{
			name:     "say forwards text only",
			tool:     "say",
			args:     map[string]any{"text": "Howdy, partner."},
			wantArgs: map[string]any{"text": "Howdy, partner."},
		},
		{
			name:     "say forwards target and emotion",
			tool:     "say",
			args:     map[string]any{"text": "Move along.", "target": "player", "emotion": "annoyed"},
			wantArgs: map[string]any{"text": "Move along.", "target": "player", "emotion": "annoyed"},
		},
		{
			name: "say omits absent optional arguments",
			tool: "say",
			args: map[string]any{"text": "Hm."},
			// target/emotion must not appear as empty strings.
			wantArgs: map[string]any{"text": "Hm."},
		},
		{
			name: "say fails without text",
			tool: "say",
			args: map[string]any{"target": "player"},
		},
		{
			name: "say fails on blank text",
			tool: "say",
			args: map[string]any{"text": "   "},
		},
		{
			name:     "gesture defaults type to neutral",
			tool:     "gesture",
			args:     map[string]any{},
			wantArgs: map[string]any{"type": "neutral"},
		},
		{
			name:     "gesture forwards a valid enum value",
			tool:     "gesture",
			args:     map[string]any{"type": "wave"},
			wantArgs: map[string]any{"type": "wave"},
		},
		{
			name:     "gesture passes an out-of-enum value through like Python",
			tool:     "gesture",
			args:     map[string]any{"type": "shrug_aggressively"},
			wantArgs: map[string]any{"type": "shrug_aggressively"},
		},
	})
}
