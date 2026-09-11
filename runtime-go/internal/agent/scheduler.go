package agent

import (
	"context"
	"sort"
	"strings"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/state"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/timeline"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/tools"
)

// scheduleReasoning builds the agent context, asks the backend for a decision,
// and executes it. It returns nil (without consulting the backend) when the
// NPC is already reasoning, is not allowed to reason, or the reasoning
// scheduler is at its configured cap.
//
// The caller must hold r.mu.
func (r *Runtime) scheduleReasoning(ctx context.Context, npcID, reason string, trigger *domain.Event) (*domain.AgentDecision, error) {
	st := r.agentStateLocked(npcID)
	if st.Reasoning {
		return nil, nil
	}
	if !r.ownership.CanReason(npcID) {
		return nil, nil
	}
	active := 0
	for _, other := range r.agentState {
		if other.Reasoning {
			active++
		}
	}
	if active >= r.settings.Scheduler.MaxReasoningAgents {
		return nil, nil
	}
	st.Reasoning = true
	st.LastDecisionAt = r.now()
	defer func() { st.Reasoning = false }()

	if err := r.emitWaitGesture(ctx, npcID, reason, trigger); err != nil {
		return nil, err
	}
	agentContext := r.buildContextLocked(npcID, reason, trigger)
	decision, err := r.callBackend(ctx, agentContext)
	if err != nil {
		return nil, err
	}
	if !st.DialogueOnly {
		decision = r.applyBehaviorGuardrails(npcID, decision, agentContext)
	}
	decision = r.applySpeechPolicy(npcID, decision, reason)
	if _, err := r.executeDecision(ctx, npcID, decision); err != nil {
		return nil, err
	}
	st.LastDecisionAt = r.now()
	return decision, nil
}

// BuildContext assembles everything a backend may see for one NPC. The
// optional trigger event is the map form of a timeline event.
func (r *Runtime) BuildContext(npcID string, reason string, triggerEvent map[string]any) *domain.AgentContext {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buildContextLocked(npcID, reason, triggerEventFromMap(triggerEvent, npcID))
}

// buildContextLocked mirrors “NpcAgentRuntime.build_context“.
// The caller must hold r.mu.
func (r *Runtime) buildContextLocked(npcID, reason string, trigger *domain.Event) *domain.AgentContext {
	st := r.agentStateLocked(npcID)
	recentEvents, err := r.timeline.RecentEvents(npcID, 20)
	recent := make([]map[string]any, 0, len(recentEvents))
	if err == nil {
		for _, event := range recentEvents {
			recent = append(recent, event.ToMap())
		}
	}
	profile := r.profiles.Get(npcID)
	worldState := r.world.GetMap(npcID)
	flags := map[string]any{
		"reason":          reason,
		"ownership_state": ownershipStateName(r.ownership.State(npcID)),
		"thinking_mode":   r.thinkingModeFor(reason, trigger),
	}
	if trigger != nil {
		flags["trigger_event"] = trigger.ToMap()
	}
	if st.DialogueOnly {
		flags["dialogue_only"] = true
	}
	agentContext := domain.NewAgentContext(npcID)
	agentContext.Profile = profile
	agentContext.WorldState = worldState
	agentContext.CurrentGoal = st.CurrentGoal
	agentContext.CurrentMood = st.Mood
	agentContext.RecentEvents = recent
	agentContext.WikiContext = r.lore.ContextForProfileWithWorld(profile, worldState, 5, 1200)
	if st.DialogueOnly {
		agentContext.QuestContext = copyMap(st.QuestContext)
	}
	agentContext.AvailableTools = r.availableToolsFor(npcID)
	agentContext.Flags = flags
	return agentContext
}

// availableToolsFor hides body-controlling tools from prompt and backend while
// the quest dialogue overlay is active.
// The caller must hold r.mu.
func (r *Runtime) availableToolsFor(npcID string) []string {
	st := r.agentStateLocked(npcID)
	names := r.registry.Names()
	if !st.DialogueOnly {
		return names
	}
	allowed := map[string]bool{
		"say":             true,
		"get_world_state": true,
		"grab_timeline":   true,
		"get_world_lore":  true,
	}
	filtered := []string{}
	for _, name := range names {
		if allowed[name] {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

// emitWaitGesture plays a short thinking/listening gesture while the backend
// generates. This is a soft overlay and is marked as a transient action, so it
// does not block idle planning while the real decision is being generated.
// The caller must hold r.mu.
func (r *Runtime) emitWaitGesture(ctx context.Context, npcID, reason string, trigger *domain.Event) error {
	if !r.settings.ThinkingPolicy.WaitGestures {
		return nil
	}
	if r.agentStateLocked(npcID).DialogueOnly {
		// Rockstar owns the body; no runtime-injected animations.
		return nil
	}
	if r.backend == nil || !r.backend.SupportsWaitGestures() {
		return nil
	}
	switch r.ownership.State(npcID) {
	case ownershipAIActive, ownershipAIConversation:
	default:
		return nil
	}
	style := waitGestureStyle(reason, trigger)
	_, err := r.callTool(ctx, "think", r.toolContext(npcID), map[string]any{
		"style":    style,
		"duration": 2.5,
	})
	return err
}

// waitGestureStyle mirrors “_wait_gesture_style“.
func waitGestureStyle(reason string, trigger *domain.Event) string {
	eventName := ""
	if trigger != nil {
		eventName = trigger.EventName
	}
	if eventName == "PLAYER_SPOKE" || reason == "player_spoke" || reason == "push_to_talk" {
		return "listen"
	}
	switch eventName {
	case "GUNSHOT_HEARD", "PLAYER_THREATENED_NPC", "PLAYER_ATTACKED_NPC", "NPC_DAMAGED", "FIGHT_STARTED":
		return "alert"
	case "PLAYER_APPROACHED", "PLAYER_LOOKED_AT_NPC":
		return "think"
	}
	if strings.HasPrefix(reason, "action_failed") || strings.HasPrefix(reason, "idle") {
		return "ponder"
	}
	return "think"
}

// thinkingModeFor chooses low vs disabled reasoning per event using the
// configured thinking policy.
func (r *Runtime) thinkingModeFor(reason string, trigger *domain.Event) string {
	policy := r.settings.ThinkingPolicy
	defaultMode := policy.DefaultMode

	disabledReasons := policy.DisabledForReasons
	for _, disabled := range disabledReasons {
		if reason == disabled {
			return "disabled"
		}
	}
	for _, prefix := range disabledReasons {
		if prefix != "" && strings.HasPrefix(reason, prefix) {
			return "disabled"
		}
	}

	eventName := ""
	if trigger != nil {
		eventName = trigger.EventName
	}
	for _, disabled := range policy.DisabledForEvents {
		if eventName == disabled {
			return "disabled"
		}
	}

	if defaultMode == "low" || defaultMode == "disabled" {
		return defaultMode
	}
	return "low"
}

// toolContext builds the tool context handed to registry handlers.
// The caller must hold r.mu.
func (r *Runtime) toolContext(npcID string) *tools.Context {
	return &tools.Context{
		NPCID:      npcID,
		World:      r.world,
		Timeline:   r.timeline,
		Lore:       r.lore,
		Profile:    r.profiles.Get(npcID),
		Dispatcher: r.dispatcher,
	}
}

// reactToEvent mirrors “_react_to_event“: the fact-driven half of the
// runtime.
// The caller must hold r.mu.
func (r *Runtime) reactToEvent(ctx context.Context, event domain.Event) error {
	st := r.agentStateLocked(event.NPCID)
	switch event.EventName {
	case "PLAYER_SPOKE":
		// A Push-to-Talk start already puts the NPC in AI_CONVERSATION.
		// Player speech during that conversation must not duplicate the
		// CONVERSATION_STARTED event.
		if err := r.ensureSingleConversation(ctx, event.NPCID); err != nil {
			return err
		}
		if r.ownership.State(event.NPCID) != ownershipAIConversation {
			if transition := r.ownership.PushToTalk(event.NPCID); transition != nil {
				if _, err := r.recordTransition(ctx, transition, nil, nil); err != nil {
					return err
				}
			}
		}
		st.ConversationActive = true
		_, err := r.scheduleReasoning(ctx, event.NPCID, "player_spoke", &event)
		return err
	case "PLAYER_THREATENED_NPC", "PLAYER_ATTACKED_NPC", "NPC_DAMAGED",
		"GUNSHOT_HEARD", "DEAD_BODY_DISCOVERED", "FIGHT_STARTED":
		return r.makeMeaningful(ctx, event.NPCID, "meaningful_event:"+event.EventName, &event)
	case "PLAYER_APPROACHED", "PLAYER_LOOKED_AT_NPC":
		if st.DialogueOnly {
			// Quest NPC: generate a dialogue-only line tied to the quest,
			// while Rockstar keeps movement/tasks.
			_, err := r.scheduleReasoning(ctx, event.NPCID, "quest_dialogue", &event)
			return err
		}
		if r.ownership.State(event.NPCID) == ownershipAware {
			// These make an aware NPC active only when the owner state allows.
			return r.makeMeaningful(ctx, event.NPCID, "meaningful_event:"+event.EventName, &event)
		}
		return nil
	case "ACTION_FAILED":
		_, err := r.scheduleReasoning(ctx, event.NPCID, "action_failed", &event)
		return err
	case "GOAL_COMPLETED":
		st.CurrentGoal = nil
		return nil
	case "CONVERSATION_ENDED", "CONVERSATION_INTERRUPTED":
		r.ownership.ConversationEnded(event.NPCID)
		st.ConversationActive = false
		return nil
	}
	return nil
}

// makeMeaningful promotes an AWARE NPC when a meaningful trigger arrives,
// deferring it when the active-agent scheduler is full.
// The caller must hold r.mu.
func (r *Runtime) makeMeaningful(ctx context.Context, npcID, reason string, trigger *domain.Event) error {
	if r.ownership.State(npcID) == ownershipAware {
		if !r.canPromoteAgent(npcID) {
			r.setDeferred(npcID, reason)
			if _, err := r.appendEvent(npcID, "AGENT_ACTIVATION_DEFERRED", timeline.AppendOptions{
				Data: map[string]any{
					"reason":            "scheduler_cap",
					"active_agents":     r.activeAgentCount(),
					"max_active_agents": r.maxActiveAgents(),
				},
				Tags:       []string{"ownership", "scheduler"},
				Importance: float64Pointer(0.3),
			}); err != nil {
				return err
			}
			return nil
		}
		transition := r.ownership.UpdateFromScan(state.ScanUpdate{
			NPCID:              npcID,
			DistanceM:          0,
			Eligible:           true,
			CandidateDistanceM: r.settings.Activation.CandidateDistanceM,
			AwareDistanceM:     r.settings.Activation.AwareDistanceM,
			MeaningfulTrigger:  true,
		})
		if transition != nil {
			if _, err := r.recordTransition(ctx, transition, nil, nil); err != nil {
				return err
			}
		}
	}
	if r.ownership.CanReason(npcID) {
		_, err := r.scheduleReasoning(ctx, npcID, reason, trigger)
		return err
	}
	return nil
}

// activeAgentCount counts NPCs in AI_ACTIVE or AI_CONVERSATION.
// The caller must hold r.mu.
func (r *Runtime) activeAgentCount() int {
	active := 0
	for _, npcID := range r.ownedNPCIDs() {
		switch r.ownership.State(npcID) {
		case ownershipAIActive, ownershipAIConversation:
			active++
		}
	}
	return active
}

// maxActiveAgents mirrors “_max_active_agents“.
func (r *Runtime) maxActiveAgents() int {
	return r.settings.Scheduler.MaxReasoningAgents
}

// canPromoteAgent reports whether the NPC may take an active-agent slot.
// The caller must hold r.mu.
func (r *Runtime) canPromoteAgent(npcID string) bool {
	switch r.ownership.State(npcID) {
	case ownershipAIActive, ownershipAIConversation:
		return true
	}
	return r.activeAgentCount() < r.maxActiveAgents()
}

// promoteNextDeferred promotes the oldest deferred AWARE NPC when a reasoning
// slot frees up.
// The caller must hold r.mu.
func (r *Runtime) promoteNextDeferred(ctx context.Context) error {
	for len(r.deferredOrder) > 0 && r.activeAgentCount() < r.maxActiveAgents() {
		npcID, reason, ok := r.popDeferred()
		if !ok {
			return nil
		}
		if r.ownership.State(npcID) != ownershipAware {
			continue
		}
		if err := r.makeMeaningful(ctx, npcID, "deferred_promotion:"+reason, nil); err != nil {
			return err
		}
	}
	return nil
}

// setDeferred records a deferred activation; re-deferring an NPC keeps its
// original queue position, matching Python's OrderedDict assignment.
// The caller must hold r.mu.
func (r *Runtime) setDeferred(npcID, reason string) {
	if _, exists := r.deferredReason[npcID]; !exists {
		r.deferredOrder = append(r.deferredOrder, npcID)
	}
	r.deferredReason[npcID] = reason
}

// removeDeferred forgets a deferred activation.
// The caller must hold r.mu.
func (r *Runtime) removeDeferred(npcID string) {
	if _, exists := r.deferredReason[npcID]; !exists {
		return
	}
	delete(r.deferredReason, npcID)
	for index, candidate := range r.deferredOrder {
		if candidate == npcID {
			r.deferredOrder = append(r.deferredOrder[:index], r.deferredOrder[index+1:]...)
			return
		}
	}
}

// popDeferred removes and returns the oldest queued NPC.
// The caller must hold r.mu.
func (r *Runtime) popDeferred() (string, string, bool) {
	for len(r.deferredOrder) > 0 {
		npcID := r.deferredOrder[0]
		r.deferredOrder = r.deferredOrder[1:]
		if reason, ok := r.deferredReason[npcID]; ok {
			delete(r.deferredReason, npcID)
			return npcID, reason, true
		}
	}
	return "", "", false
}

// ensureSingleConversation enforces the max_conversation_agents scheduler cap
// by ending the oldest excess conversations.
// The caller must hold r.mu.
func (r *Runtime) ensureSingleConversation(ctx context.Context, npcID string) error {
	maxConversations := r.settings.Scheduler.MaxConversationAgents
	active := []string{}
	for _, otherID := range r.ownedNPCIDs() {
		if otherID == npcID {
			continue
		}
		if r.ownership.State(otherID) == ownershipAIConversation {
			active = append(active, otherID)
		}
	}
	if len(active) < maxConversations {
		return nil
	}
	// Keep at most max_conversations; stop the oldest excess conversations.
	stopCount := len(active) - maxConversations + 1
	if stopCount < 0 {
		stopCount = 0
	}
	if stopCount > len(active) {
		stopCount = len(active)
	}
	for _, otherID := range active[:stopCount] {
		if transition := r.ownership.ConversationEnded(otherID); transition != nil {
			if _, err := r.recordTransition(ctx, transition, nil, nil); err != nil {
				return err
			}
		}
		if _, err := r.appendEvent(otherID, "CONVERSATION_ENDED", timeline.AppendOptions{
			Data:       map[string]any{"by": "scheduler", "reason": "max_conversation_agents"},
			Tags:       []string{"conversation", "scheduler"},
			Importance: float64Pointer(0.3),
		}); err != nil {
			return err
		}
		r.agentStateLocked(otherID).ConversationActive = false
	}
	return nil
}

// recordTransition persists an ownership transition and performs the
// RELEASE hand-off cleanup.
// The caller must hold r.mu.
func (r *Runtime) recordTransition(ctx context.Context, transition *state.Transition, ped *domain.PedSnapshot, reasons []string) (map[string]any, error) {
	eventName, _ := transition.EventName()
	if eventName == "" && transition.To == ownershipAIConversation {
		eventName = "CONVERSATION_STARTED"
	}
	if eventName != "" {
		data := map[string]any{
			"from_state": ownershipStateName(transition.From),
			"to_state":   ownershipStateName(transition.To),
			"reason":     transition.Reason,
		}
		if ped != nil {
			data["entity"] = ped.EntityID
			if ped.Model != "" {
				data["model"] = ped.Model
			}
		}
		if len(reasons) > 0 {
			data["eligibility_reasons"] = reasons
		}
		importance := 0.3
		switch transition.To {
		case ownershipAIActive, ownershipRelease, ownershipSuspended:
			importance = 0.8
		}
		if _, err := r.appendEvent(transition.NPCID, eventName, timeline.AppendOptions{
			Data:       data,
			Tags:       []string{"ownership"},
			Importance: &importance,
		}); err != nil {
			return nil, err
		}
	}
	// RELEASE is a transient hand-off state; cleanup returns ownership to
	// Rockstar immediately after the release event is recorded.
	if transition.To == ownershipRelease || transition.To == ownershipRockstar {
		r.removeDeferred(transition.NPCID)
		runtimeState := r.agentStateLocked(transition.NPCID)
		runtimeState.DialogueOnly = false
		runtimeState.QuestContext = map[string]any{}
	}
	if transition.To == ownershipRelease {
		r.ownership.FinalizeRelease(transition.NPCID)
		if err := r.promoteNextDeferred(ctx); err != nil {
			return nil, err
		}
	}
	var eventNameValue any
	if eventName != "" {
		eventNameValue = eventName
	}
	return map[string]any{
		"npc_id":     transition.NPCID,
		"from":       ownershipStateName(transition.From),
		"to":         ownershipStateName(transition.To),
		"reason":     transition.Reason,
		"event_name": eventNameValue,
	}, nil
}

// ReleaseAll releases every agent-owned NPC back to Rockstar AI.
func (r *Runtime) ReleaseAll(ctx context.Context, reason string) error {
	ctx = nonNilContext(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, npcID := range r.ownedNPCIDs() {
		switch r.ownership.State(npcID) {
		case ownershipRockstar, ownershipRelease:
			continue
		}
		transition := r.ownership.RequestRelease(npcID, reason)
		if transition == nil {
			continue
		}
		if _, err := r.recordTransition(ctx, transition, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// maybeScheduleIdleThinking fires an autonomous planning turn after a period
// of inactivity. This implements the spec's "autonomous planning timer" so an
// AI_ACTIVE NPC can decide to speak, change goals, or continue silently while
// the player is still nearby.
// The caller must hold r.mu.
func (r *Runtime) maybeScheduleIdleThinking(ctx context.Context) error {
	idleSeconds := r.settings.Scheduler.IdleThinkingSeconds
	if idleSeconds <= 0 {
		return nil
	}
	now := r.now()
	busy := r.dispatcher.PendingNPCIDs()
	for _, npcID := range r.agentStateIDsLocked() {
		st := r.agentState[npcID]
		if st.Reasoning || busy[npcID] || st.DialogueOnly {
			continue
		}
		if r.ownership.State(npcID) != ownershipAIActive {
			continue
		}
		if _, tracked := r.lastPedScan[npcID]; !tracked {
			continue
		}
		if now-st.LastDecisionAt < idleSeconds {
			continue
		}
		if _, err := r.scheduleReasoning(ctx, npcID, "idle_autonomous", nil); err != nil {
			return err
		}
	}
	return nil
}

// Tick runs the scheduler work the Python runtime drove from bridge traffic:
// idle autonomous thinking plus promotion of deferred activations.
//
// Errors are not reported because the required signature has no error return;
// the runtime stays consistent either way.
func (r *Runtime) Tick(ctx context.Context) {
	ctx = nonNilContext(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.maybeScheduleIdleThinking(ctx)
	_ = r.promoteNextDeferred(ctx)
}

// selectConversationTarget picks the best nearby NPC to talk to, mirroring
// “select_conversation_target“. It returns "" when no candidate qualifies.
// The caller must hold r.mu.
func (r *Runtime) selectConversationTarget() string {
	type candidate struct {
		score float64
		npcID string
	}
	candidates := []candidate{}
	maxRange := r.settings.Activation.ConversationRangeM
	for _, npcID := range r.pedScanIDsLocked() {
		ped := r.lastPedScan[npcID]
		if ped.EntityID == "" || ped.DistanceM == nil {
			continue
		}
		distance := *ped.DistanceM
		if distance > maxRange {
			continue
		}
		ownershipState := r.ownership.State(ped.EntityID)
		if ownershipState == ownershipRockstar || ownershipState == ownershipRelease {
			continue
		}
		if ownershipState != ownershipQuestDialogue {
			if !r.eligibility.CanActivate(ped).Eligible {
				continue
			}
		}
		alignment := 0.0
		if ped.Metadata != nil {
			if value, ok := coerceFloat(ped.Metadata["camera_alignment"]); ok {
				alignment = value
			}
		}
		distanceScore := 1.0 - (distance / maxRange)
		if distanceScore < 0 {
			distanceScore = 0
		}
		existing := 0.0
		if r.agentStateLocked(ped.EntityID).ConversationActive {
			existing = 0.15
		}
		score := 0.5*alignment + 0.3*distanceScore + existing
		candidates = append(candidates, candidate{score: score, npcID: ped.EntityID})
	}
	if len(candidates) == 0 {
		return ""
	}
	// Python sorts (score, npc_id) tuples descending.
	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].score != candidates[right].score {
			return candidates[left].score > candidates[right].score
		}
		return candidates[left].npcID > candidates[right].npcID
	})
	return candidates[0].npcID
}

// questDialoguePayload returns the quest context when the ped is flagged for
// the dialogue-only overlay, or nil when it is not eligible.
// The caller must hold r.mu.
func (r *Runtime) questDialoguePayload(ped domain.PedSnapshot) map[string]any {
	metadata := ped.Metadata
	questID := domain.StringFrom(metadata["quest_id"])
	if _, present := metadata["quest_id"]; !present || metadata["quest_id"] == nil {
		questID = domain.StringFrom(r.profiles.Get(ped.EntityID)["quest_id"])
	}
	dialogueFlag := false
	if raw, present := metadata["quest_dialogue"]; present {
		dialogueFlag = domain.BoolFrom(raw)
	} else if raw, present := metadata["dialogue_only"]; present {
		dialogueFlag = domain.BoolFrom(raw)
	}
	if questID == "" || !dialogueFlag {
		return nil
	}
	// Conservative base checks: never overlay a dead, player, non-human, or
	// cutscene entity. The quest_dialogue flag only relaxes the
	// mission/story blacklist, not these.
	if ped.IsPed == nil || !*ped.IsPed || ped.IsHuman == nil || !*ped.IsHuman || ped.IsAlive == nil || !*ped.IsAlive {
		return nil
	}
	if ped.IsPlayer != nil && *ped.IsPlayer {
		return nil
	}
	if ped.InCutscene == nil || *ped.InCutscene {
		return nil
	}
	runtimeState := domain.MapFrom(metadata["quest_state"])
	if len(runtimeState) == 0 {
		runtimeState = domain.MapFrom(metadata["quest"])
	}
	return r.quests.ContextFor(questID, runtimeState)
}

// handleQuestDialoguePed allows a dialogue-only overlay for Rockstar-controlled
// quest NPCs. Movement, tasks, and animation remain owned by Rockstar; only
// speech from our agent runtime is allowed while the NPC is in range.
// The caller must hold r.mu.
func (r *Runtime) handleQuestDialoguePed(ctx context.Context, ped domain.PedSnapshot, distance, candidateDistanceM, awareDistanceM float64) (map[string]any, error) {
	st := r.agentStateLocked(ped.EntityID)
	payload := r.questDialoguePayload(ped)

	if payload == nil {
		if st.DialogueOnly {
			reason := "quest_dialogue_ended"
			if distance > candidateDistanceM {
				reason = "quest_dialogue_left_area"
			}
			transition := r.ownership.RequestRelease(ped.EntityID, reason)
			if transition != nil {
				return r.recordTransition(ctx, transition, &ped, nil)
			}
		}
		return nil, nil
	}

	st.DialogueOnly = true
	st.QuestContext = payload
	if distance > candidateDistanceM {
		switch r.ownership.State(ped.EntityID) {
		case ownershipQuestDialogue, ownershipAIActive, ownershipAIConversation:
			transition := r.ownership.RequestRelease(ped.EntityID, "quest_dialogue_left_area")
			if transition != nil {
				return r.recordTransition(ctx, transition, &ped, nil)
			}
		}
		return nil, nil
	}
	if distance <= awareDistanceM {
		transition := r.ownership.EnterQuestDialogue(ped.EntityID)
		if transition != nil {
			return r.recordTransition(ctx, transition, &ped, nil)
		}
	}
	return nil, nil
}

// ownedNPCIDs lists every NPC the ownership manager tracks, in first-seen
// order (the manager mirrors Python's insertion-ordered record dict).
// The caller must hold r.mu.
func (r *Runtime) ownedNPCIDs() []string {
	return r.ownership.OwnedNPCs()
}

// pedScanIDsLocked lists tracked ped ids in deterministic order.
// The caller must hold r.mu.
func (r *Runtime) pedScanIDsLocked() []string {
	ids := make([]string, 0, len(r.lastPedScan))
	for npcID := range r.lastPedScan {
		ids = append(ids, npcID)
	}
	sort.Strings(ids)
	return ids
}

// triggerEventFromMap leniently converts a map into a trigger event. Unlike
// domain.EventFromMap it tolerates missing sequence numbers and ids, because
// the exported BuildContext accepts raw event maps.
func triggerEventFromMap(raw map[string]any, defaultNPCID string) *domain.Event {
	if len(raw) == 0 {
		return nil
	}
	event := domain.NewEvent(domain.NewEventOptions{
		Seq:       domain.IntFrom(raw["seq"]),
		EventName: domain.StringFrom(raw["event_name"]),
		NPCID:     defaultNPCID,
		EventID:   domain.StringFrom(raw["event_id"]),
		Data:      domain.MapFrom(raw["data"]),
		Entities:  domain.StringsFrom(raw["entities"]),
		Tags:      domain.StringsFrom(raw["tags"]),
	})
	if npcID := domain.StringFrom(raw["npc_id"]); npcID != "" {
		event.NPCID = npcID
	}
	if raw["summary"] != nil {
		summary := domain.StringFrom(raw["summary"])
		event.Summary = &summary
	}
	if raw["importance"] != nil {
		importance := domain.FloatFrom(raw["importance"])
		event.Importance = &importance
	}
	if raw["timestamp"] != nil {
		timestamp := domain.FloatFrom(raw["timestamp"])
		event.Timestamp = timestamp
	}
	if raw["game_time"] != nil {
		event.GameTime = domain.GameTimeFromMap(domain.MapFrom(raw["game_time"]))
	}
	if raw["location"] != nil {
		event.Location = domain.LocationFromMap(domain.MapFrom(raw["location"]))
	}
	return &event
}
