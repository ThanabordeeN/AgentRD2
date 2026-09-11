package domain

// AgentContext is everything a backend is allowed to see when deciding.
type AgentContext struct {
	NPCID           string           `json:"npc_id"`
	Profile         map[string]any   `json:"profile"`
	WorldState      map[string]any   `json:"world_state"`
	CurrentGoal     *string          `json:"current_goal"`
	CurrentMood     string           `json:"current_mood"`
	RecentEvents    []map[string]any `json:"recent_events"`
	RetrievedEvents []map[string]any `json:"retrieved_events"`
	WikiContext     []map[string]any `json:"wiki_context"`
	QuestContext    map[string]any   `json:"quest_context"`
	AvailableTools  []string         `json:"available_tools"`
	Flags           map[string]any   `json:"flags"`
}

// NewAgentContext mirrors the Python dataclass defaults.
func NewAgentContext(npcID string) *AgentContext {
	return &AgentContext{
		NPCID:           npcID,
		CurrentMood:     "neutral",
		RecentEvents:    []map[string]any{},
		RetrievedEvents: []map[string]any{},
		WikiContext:     []map[string]any{},
		QuestContext:    map[string]any{},
		AvailableTools:  []string{},
		Flags:           map[string]any{},
	}
}

// ToMap mirrors ``AgentContext.to_dict``.
func (c AgentContext) ToMap() map[string]any {
	return map[string]any{
		"npc_id":           c.NPCID,
		"profile":          nonNilMap(c.Profile),
		"world_state":      nonNilMap(c.WorldState),
		"current_goal":     c.CurrentGoal,
		"current_mood":     c.CurrentMood,
		"recent_events":    nonNilMaps(c.RecentEvents),
		"retrieved_events": nonNilMaps(c.RetrievedEvents),
		"wiki_context":     nonNilMaps(c.WikiContext),
		"quest_context":    nonNilMap(c.QuestContext),
		"available_tools":  c.AvailableTools,
		"flags":            nonNilMap(c.Flags),
	}
}

// Flag reads a context flag with a default.
func (c AgentContext) Flag(name string) any {
	if c.Flags == nil {
		return nil
	}
	return c.Flags[name]
}

// FlagString reads a string flag with a default.
func (c AgentContext) FlagString(name, fallback string) string {
	if value, ok := c.Flag(name).(string); ok && value != "" {
		return value
	}
	return fallback
}

// Goalless reports whether the NPC has no active goal.
func (c AgentContext) Goalless() bool {
	return c.CurrentGoal == nil || *c.CurrentGoal == ""
}

// AgentSpeech is an optional spoken line. A nil speech means silence.
type AgentSpeech struct {
	Text    string `json:"text"`
	Target  string `json:"target,omitempty"`
	Emotion string `json:"emotion,omitempty"`
}

// ToMap mirrors ``AgentSpeech.to_dict``.
func (s AgentSpeech) ToMap() map[string]any {
	payload := map[string]any{"text": s.Text}
	if s.Target != "" {
		payload["target"] = s.Target
	}
	if s.Emotion != "" {
		payload["emotion"] = s.Emotion
	}
	return payload
}

// AgentAction is one high-level tool call requested by a backend.
type AgentAction struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}

// ToMap mirrors ``AgentAction.to_dict``.
func (a AgentAction) ToMap() map[string]any {
	return map[string]any{"tool": a.Tool, "arguments": CleanMap(a.Arguments)}
}

// AgentDecision is the structured output produced by a backend.
type AgentDecision struct {
	Goal     *string        `json:"goal"`
	Mood     *string        `json:"mood"`
	Speech   *AgentSpeech   `json:"speech"`
	Actions  []AgentAction  `json:"actions"`
	Internal map[string]any `json:"internal"`
}

// NewAgentDecision returns an empty decision (silence, no actions).
func NewAgentDecision() *AgentDecision {
	return &AgentDecision{Actions: []AgentAction{}, Internal: map[string]any{}}
}

// GoalText returns the goal or an empty string.
func (d AgentDecision) GoalText() string {
	if d.Goal == nil {
		return ""
	}
	return *d.Goal
}

// MoodText returns the mood or an empty string.
func (d AgentDecision) MoodText() string {
	if d.Mood == nil {
		return ""
	}
	return *d.Mood
}

// SpeechText returns the spoken line or an empty string.
func (d AgentDecision) SpeechText() string {
	if d.Speech == nil {
		return ""
	}
	return d.Speech.Text
}

// ToolNames lists the tools the decision wants to run.
func (d AgentDecision) ToolNames() []string {
	names := make([]string, 0, len(d.Actions))
	for _, action := range d.Actions {
		names = append(names, action.Tool)
	}
	return names
}

// ToMap mirrors ``AgentDecision.to_dict``: ``internal`` always carries goal and
// mood, and nil speech is dropped entirely.
func (d AgentDecision) ToMap() map[string]any {
	internal := map[string]any{}
	if d.Goal != nil {
		internal["goal"] = *d.Goal
	}
	if d.Mood != nil {
		internal["mood"] = *d.Mood
	}
	for key, value := range d.Internal {
		if value == nil {
			continue
		}
		internal[key] = value
	}

	actions := make([]any, 0, len(d.Actions))
	for _, action := range d.Actions {
		actions = append(actions, action.ToMap())
	}

	payload := map[string]any{
		"internal": CleanMap(internal),
		"actions":  actions,
	}
	if d.Speech != nil {
		payload["speech"] = d.Speech.ToMap()
	}
	return CleanNone(payload).(map[string]any)
}
