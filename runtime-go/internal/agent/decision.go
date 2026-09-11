package agent

import (
	"context"
	"strings"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/timeline"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/tools"
)

// executeDecision records goal/mood changes and runs the decision's speech and
// actions through the tool registry.
// The caller must hold r.mu.
func (r *Runtime) executeDecision(ctx context.Context, npcID string, decision *domain.AgentDecision) ([]domain.ToolResult, error) {
	st := r.agentStateLocked(npcID)
	results := []domain.ToolResult{}
	if st.DialogueOnly {
		// Quest NPC actions belong to Rockstar. Keep only speech, and log
		// any blocked action so the timeline remains debuggable.
		allowedActions := []domain.AgentAction{}
		for _, action := range decision.Actions {
			if action.Tool == "say" {
				allowedActions = append(allowedActions, action)
				continue
			}
			if _, err := r.appendEvent(npcID, "ACTION_FAILED", timeline.AppendOptions{
				Data: map[string]any{
					"tool":    action.Tool,
					"reason":  "dialogue_only_mode",
					"message": "Rockstar owns quest NPC actions",
				},
				Tags:       []string{"action", "quest_dialogue"},
				Importance: float64Pointer(0.3),
			}); err != nil {
				return nil, err
			}
		}
		decision.Actions = allowedActions
	} else {
		stabilized, err := r.stabilizeDecision(npcID, decision)
		if err != nil {
			return nil, err
		}
		decision = stabilized
	}

	if goal := decision.GoalText(); goal != "" && (st.CurrentGoal == nil || *st.CurrentGoal != goal) {
		eventName := "GOAL_CHANGED"
		if st.CurrentGoal == nil {
			eventName = "GOAL_CREATED"
		}
		var previousGoal any
		if st.CurrentGoal != nil {
			previousGoal = *st.CurrentGoal
		}
		if _, err := r.appendEvent(npcID, eventName, timeline.AppendOptions{
			Data:       map[string]any{"previous_goal": previousGoal, "goal": goal},
			Tags:       []string{"goal"},
			Importance: float64Pointer(0.4),
		}); err != nil {
			return nil, err
		}
		goalCopy := goal
		st.CurrentGoal = &goalCopy
	}
	if mood := decision.MoodText(); mood != "" {
		st.Mood = mood
	}

	toolCtx := r.toolContext(npcID)
	if decision.Speech != nil {
		if r.speechAllowed(npcID) {
			result, err := r.callTool(ctx, "say", toolCtx, map[string]any{
				"text":    decision.Speech.Text,
				"target":  decision.Speech.Target,
				"emotion": decision.Speech.Emotion,
			})
			if err != nil {
				return nil, err
			}
			results = append(results, result)
			st.LastSpokenAt = r.now()
		}
	}

	for _, action := range decision.Actions {
		if action.Tool == "say" && decision.Speech != nil {
			// Avoid double speaking when the backend emits both.
			continue
		}
		result, err := r.callTool(ctx, action.Tool, toolCtx, action.Arguments)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	for _, result := range results {
		if result.Tool == "say" && result.Status == domain.ActionStarted {
			r.lastGlobalSpeechAt = r.now()
			break
		}
	}
	return results, nil
}

// stabilizeDecision validates and normalizes LLM tool actions before they
// reach the bridge.
//
// LLMs occasionally omit required tool arguments. The runtime fills
// safe/intention-preserving defaults where possible and drops invalid actions
// rather than sending malformed commands to RDR2.
// The caller must hold r.mu.
func (r *Runtime) stabilizeDecision(npcID string, decision *domain.AgentDecision) (*domain.AgentDecision, error) {
	if len(decision.Actions) == 0 {
		return decision, nil
	}
	normalized := make([]domain.AgentAction, 0, len(decision.Actions))
	for _, action := range decision.Actions {
		fixed, reason, ok := r.normalizeAction(npcID, action, decision)
		if ok {
			normalized = append(normalized, fixed)
			continue
		}
		if reason == "" {
			continue
		}
		if _, err := r.appendEvent(npcID, "ACTION_FAILED", timeline.AppendOptions{
			Data: map[string]any{
				"tool":      action.Tool,
				"reason":    reason,
				"arguments": copyMap(action.Arguments),
			},
			Tags:       []string{"action", "validation"},
			Importance: float64Pointer(0.3),
		}); err != nil {
			return nil, err
		}
	}
	decision.Actions = normalized
	return decision, nil
}

// normalizeAction mirrors “_normalize_action“: it returns the fixed action,
// a drop reason, and whether the action survives.
// The caller must hold r.mu.
func (r *Runtime) normalizeAction(npcID string, action domain.AgentAction, decision *domain.AgentDecision) (domain.AgentAction, string, bool) {
	tool := action.Tool
	args := map[string]any{}
	for key, value := range action.Arguments {
		if value == nil {
			continue
		}
		args[key] = value
	}

	switch tool {
	case "say":
		if decision.Speech != nil && decision.Speech.Text != "" {
			// The runtime already speaks decision.speech; avoid a duplicate.
			return domain.AgentAction{}, "", false
		}
		if strings.TrimSpace(domain.StringFrom(args["text"])) == "" {
			return domain.AgentAction{}, "say_missing_text", false
		}
		return domain.AgentAction{Tool: "say", Arguments: args}, "", true

	case "look_at", "face", "follow", "flee_from":
		if _, present := args["entity"]; !present {
			args["entity"] = "player"
		}
		return domain.AgentAction{Tool: tool, Arguments: args}, "", true

	case "wander":
		args["radius"] = floatOrDefault(args["radius"], 8.0)
		return domain.AgentAction{Tool: tool, Arguments: args}, "", true

	case "go_to":
		if !isTruthy(args["destination"]) {
			internalDestination := decision.Internal["destination"]
			if isTruthy(internalDestination) {
				args["destination"] = internalDestination
			} else if decision.Goal != nil && *decision.Goal != "" {
				// Intention-preserving fallback: wander locally rather than
				// sending a go_to with no destination.
				return domain.AgentAction{Tool: "wander", Arguments: map[string]any{"radius": 8.0}}, "", true
			} else {
				return domain.AgentAction{}, "go_to_missing_destination", false
			}
		}
		return domain.AgentAction{Tool: tool, Arguments: args}, "", true

	case "investigate":
		position := args["position"]
		if !isTruthy(position) {
			position = r.recentPosition(npcID)
		}
		if !isTruthy(position) {
			return domain.AgentAction{}, "investigate_missing_position", false
		}
		args["position"] = position
		return domain.AgentAction{Tool: tool, Arguments: args}, "", true

	case "wait":
		args["duration"] = floatOrDefault(args["duration"], 2.0)
		return domain.AgentAction{Tool: tool, Arguments: args}, "", true

	case "gesture":
		if _, present := args["type"]; !present {
			args["type"] = "neutral"
		}
		return domain.AgentAction{Tool: tool, Arguments: args}, "", true

	case "stop", "clear_attention":
		return domain.AgentAction{Tool: tool, Arguments: map[string]any{}}, "", true
	}
	return domain.AgentAction{}, "unsupported_tool", false
}

// recentPosition scans the newest events for a usable [x, y, z] position.
// The caller must hold r.mu.
func (r *Runtime) recentPosition(npcID string) []float64 {
	events, err := r.timeline.RecentEvents(npcID, 30)
	if err != nil {
		return nil
	}
	for index := len(events) - 1; index >= 0; index-- {
		data := events[index].Data
		for _, key := range []string{"position", "target_position", "destination_position"} {
			raw, present := data[key]
			if !present {
				continue
			}
			if position, ok := coercePosition(raw); ok {
				return position
			}
		}
	}
	return nil
}

// callTool invokes one registry handler, filtering nil arguments the way the
// Python “_call_tool“ did and converting handler panics into failed results
// so an action failure never kills the runtime.
// The caller must hold r.mu.
func (r *Runtime) callTool(ctx context.Context, name string, toolCtx *tools.Context, arguments map[string]any) (domain.ToolResult, error) {
	filtered := map[string]any{}
	for key, value := range arguments {
		if value == nil {
			continue
		}
		filtered[key] = value
	}
	if !r.registryHas(name) {
		request := domain.NewActionRequest(name, filtered, toolCtx.NPCID)
		if toolCtx.NPCID != "" {
			if _, err := r.appendEvent(toolCtx.NPCID, "ACTION_FAILED", timeline.AppendOptions{
				Data: map[string]any{
					"tool":    name,
					"reason":  "KeyError",
					"message": "unknown agent tool: " + name,
				},
				Tags:       []string{"action"},
				Importance: float64Pointer(0.5),
			}); err != nil {
				return domain.ToolResult{}, err
			}
		}
		return domain.FailedResult(request, "unknown agent tool: "+name, nil), nil
	}
	return r.registryCall(name, toolCtx, filtered)
}

// registryCall isolates the registry call so a panicking handler becomes a
// failed result instead of a crash.
func (r *Runtime) registryCall(name string, toolCtx *tools.Context, arguments map[string]any) (result domain.ToolResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			request := domain.NewActionRequest(name, arguments, toolCtx.NPCID)
			if toolCtx.NPCID != "" {
				if _, appendErr := r.appendEvent(toolCtx.NPCID, "ACTION_FAILED", timeline.AppendOptions{
					Data: map[string]any{
						"tool":    name,
						"reason":  "panic",
						"message": pyStr(recovered),
					},
					Tags:       []string{"action"},
					Importance: float64Pointer(0.5),
				}); appendErr != nil {
					err = appendErr
					return
				}
			}
			result = domain.FailedResult(request, pyStr(recovered), nil)
		}
	}()
	result = r.registry.Call(name, toolCtx, arguments)
	return result, nil
}

// applyBehaviorGuardrails guarantees critical spec behaviors even if the LLM
// omits an action.
//
// Speech remains LLM-authored. The guardrail only ensures that urgent events
// produce at least one appropriate high-level action.
// The caller must hold r.mu.
func (r *Runtime) applyBehaviorGuardrails(npcID string, decision *domain.AgentDecision, agentContext *domain.AgentContext) *domain.AgentDecision {
	trigger := domain.MapFrom(agentContext.Flags["trigger_event"])
	eventName := domain.StringFrom(trigger["event_name"])
	data := domain.MapFrom(trigger["data"])
	profile := r.profiles.Get(npcID)
	personality := domain.MapFrom(profile["personality"])
	courage := 0.5
	if raw, present := personality["courage"]; present {
		if parsed, ok := coerceFloat(raw); ok {
			courage = parsed
		}
	}
	actions := append([]domain.AgentAction{}, decision.Actions...)
	tools := toolSet(actions)

	switch eventName {
	case "PLAYER_THREATENED_NPC", "PLAYER_ATTACKED_NPC", "NPC_DAMAGED":
		if courage < 0.48 {
			// Low-courage NPC must try to escape. Remove conflicting movement
			// intentions and put flee_from first.
			actions = filterActions(actions, map[string]bool{
				"go_to": true, "follow": true, "wander": true, "investigate": true,
			})
			if !toolSet(actions)["flee_from"] {
				actions = append([]domain.AgentAction{{
					Tool:      "flee_from",
					Arguments: map[string]any{"entity": "player"},
				}}, actions...)
			}
			setMoodDefault(decision, "afraid")
			setGoalDefault(decision, "get away from the player")
		} else if !tools["face"] && !tools["look_at"] {
			actions = append(actions, domain.AgentAction{
				Tool:      "face",
				Arguments: map[string]any{"entity": "player"},
			})
		}
	case "GUNSHOT_HEARD":
		position := data["position"]
		if courage >= 0.55 && isPositionValue(position) {
			if !toolSet(actions)["investigate"] {
				actions = filterActions(actions, map[string]bool{
					"wander": true, "go_to": true, "follow": true, "flee_from": true,
				})
				actions = append(actions, domain.AgentAction{
					Tool:      "investigate",
					Arguments: map[string]any{"position": position},
				})
			}
			setMoodDefault(decision, "alert")
			setGoalDefault(decision, "investigate the gunshot")
		} else if courage < 0.40 && !hasAnyTool(actions, "wander", "go_to", "flee_from") {
			actions = append(actions, domain.AgentAction{
				Tool:      "wander",
				Arguments: map[string]any{"radius": 12.0},
			})
			setMoodDefault(decision, "afraid")
		}
	}
	decision.Actions = actions
	return decision
}

// applySpeechPolicy enforces the speech cooldowns unless the turn is a direct
// answer or an urgent reaction.
// The caller must hold r.mu.
func (r *Runtime) applySpeechPolicy(npcID string, decision *domain.AgentDecision, reason string) *domain.AgentDecision {
	if decision.Speech == nil {
		return decision
	}
	// Direct push-to-talk answers and urgent reactions bypass cooldown;
	// ordinary autonomous speech does not.
	if reason == "push_to_talk" || reason == "player_spoke" {
		return decision
	}
	switch reason {
	case "meaningful_event:PLAYER_THREATENED_NPC",
		"meaningful_event:PLAYER_ATTACKED_NPC",
		"meaningful_event:NPC_DAMAGED",
		"meaningful_event:GUNSHOT_HEARD":
		return decision
	}
	cooldown := r.settings.Speech.CooldownSeconds
	globalCooldown := r.settings.Speech.GlobalCooldownSeconds
	st := r.agentStateLocked(npcID)
	now := r.now()
	if now-st.LastSpokenAt < cooldown {
		decision.Speech = nil
		return decision
	}
	if now-r.lastGlobalSpeechAt < globalCooldown {
		decision.Speech = nil
	}
	return decision
}

// speechAllowed mirrors “_speech_allowed“.
// The caller must hold r.mu.
func (r *Runtime) speechAllowed(npcID string) bool {
	return r.ownership.CanSpeak(npcID)
}

// callBackend asks the configured backend for a decision.
func (r *Runtime) callBackend(ctx context.Context, agentContext *domain.AgentContext) (*domain.AgentDecision, error) {
	if r.backend == nil {
		return domain.NewAgentDecision(), nil
	}
	decision, err := r.backend.Decide(ctx, agentContext)
	if err != nil {
		return nil, err
	}
	if decision == nil {
		return domain.NewAgentDecision(), nil
	}
	return decision, nil
}

// registryHas reports whether the registry knows a tool name.
func (r *Runtime) registryHas(name string) bool {
	for _, candidate := range r.registry.Names() {
		if candidate == name {
			return true
		}
	}
	return false
}

func filterActions(actions []domain.AgentAction, blocked map[string]bool) []domain.AgentAction {
	filtered := make([]domain.AgentAction, 0, len(actions))
	for _, action := range actions {
		if blocked[action.Tool] {
			continue
		}
		filtered = append(filtered, action)
	}
	return filtered
}

func toolSet(actions []domain.AgentAction) map[string]bool {
	tools := make(map[string]bool, len(actions))
	for _, action := range actions {
		tools[action.Tool] = true
	}
	return tools
}

func hasAnyTool(actions []domain.AgentAction, names ...string) bool {
	tools := toolSet(actions)
	for _, name := range names {
		if tools[name] {
			return true
		}
	}
	return false
}

func setMoodDefault(decision *domain.AgentDecision, mood string) {
	if decision.Mood != nil && *decision.Mood != "" {
		return
	}
	value := mood
	decision.Mood = &value
}

func setGoalDefault(decision *domain.AgentDecision, goal string) {
	if decision.Goal != nil && *decision.Goal != "" {
		return
	}
	value := goal
	decision.Goal = &value
}

// floatOrDefault mirrors “float(args.get(key, fallback))“ including the
// Python behavior of falling back when the value cannot be converted.
func floatOrDefault(value any, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	parsed, ok := coerceFloat(value)
	if !ok {
		return fallback
	}
	return parsed
}

// coercePosition mirrors the Python list check in “_recent_position“: a
// sequence of at least three numeric values.
func coercePosition(value any) ([]float64, bool) {
	switch typed := value.(type) {
	case []any:
		if len(typed) < 3 {
			return nil, false
		}
		position := make([]float64, 0, 3)
		for _, item := range typed[:3] {
			number, ok := coerceFloat(item)
			if !ok {
				return nil, false
			}
			position = append(position, number)
		}
		return position, true
	case []float64:
		if len(typed) < 3 {
			return nil, false
		}
		return []float64{typed[0], typed[1], typed[2]}, true
	case []int:
		if len(typed) < 3 {
			return nil, false
		}
		return []float64{float64(typed[0]), float64(typed[1]), float64(typed[2])}, true
	}
	return nil, false
}

// isPositionValue mirrors “isinstance(position, list) and len(position) >= 3“.
func isPositionValue(value any) bool {
	switch typed := value.(type) {
	case []any:
		return len(typed) >= 3
	case []float64:
		return len(typed) >= 3
	case []int:
		return len(typed) >= 3
	}
	return false
}
