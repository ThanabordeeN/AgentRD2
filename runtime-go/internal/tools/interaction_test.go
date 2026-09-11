package tools

import "testing"

// TestInteractionTools covers the interaction family, including the
// always-present skip_idle_animation flag and int coercion of horse actions.
func TestInteractionTools(t *testing.T) {
	runDispatchCases(t, []dispatchCase{
		{
			name:     "mount forwards an entity",
			tool:     "mount",
			args:     map[string]any{"entity": "horse_1"},
			wantArgs: map[string]any{"entity": "horse_1"},
		},
		{
			name: "mount fails without an entity",
			tool: "mount",
			args: map[string]any{},
		},
		{
			name:     "dismount dispatches an empty payload",
			tool:     "dismount",
			args:     map[string]any{},
			wantArgs: map[string]any{},
		},
		{
			name:     "item_interaction forwards item and interaction",
			tool:     "item_interaction",
			args:     map[string]any{"item": "campfire", "interaction": "cook"},
			wantArgs: map[string]any{"item": "campfire", "interaction": "cook"},
		},
		{
			name: "item_interaction fails without an interaction",
			tool: "item_interaction",
			args: map[string]any{"item": "campfire"},
		},
		{
			name: "item_interaction fails without an item",
			tool: "item_interaction",
			args: map[string]any{"interaction": "cook"},
		},
		{
			name: "animal_interaction defaults skip_idle_animation to false",
			tool: "animal_interaction",
			args: map[string]any{
				"target": "deer_1", "interaction_type": "skin", "interaction_model": "mp_skin",
			},
			wantArgs: map[string]any{
				"target": "deer_1", "interaction_type": "skin", "interaction_model": "mp_skin",
				"skip_idle_animation": false,
			},
		},
		{
			name: "animal_interaction forwards skip_idle_animation",
			tool: "animal_interaction",
			args: map[string]any{
				"target": "deer_1", "interaction_type": "skin", "interaction_model": "mp_skin",
				"skip_idle_animation": true,
			},
			wantArgs: map[string]any{
				"target": "deer_1", "interaction_type": "skin", "interaction_model": "mp_skin",
				"skip_idle_animation": true,
			},
		},
		{
			name: "animal_interaction fails without the interaction model",
			tool: "animal_interaction",
			args: map[string]any{"target": "deer_1", "interaction_type": "skin"},
		},
		{
			name:     "horse_action coerces the action to int",
			tool:     "horse_action",
			args:     map[string]any{"action": 7.9},
			wantArgs: map[string]any{"action": 7},
		},
		{
			name:     "horse_action accepts a numeric string",
			tool:     "horse_action",
			args:     map[string]any{"action": "3"},
			wantArgs: map[string]any{"action": 3},
		},
		{
			name:     "horse_action forwards an optional target",
			tool:     "horse_action",
			args:     map[string]any{"action": 2, "target": "horse_1"},
			wantArgs: map[string]any{"action": 2, "target": "horse_1"},
		},
		{
			name: "horse_action fails without an action",
			tool: "horse_action",
			args: map[string]any{"target": "horse_1"},
		},
		{
			name: "horse_action fails on a non-numeric action",
			tool: "horse_action",
			args: map[string]any{"action": "gallop"},
		},
	})
}
