package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/state"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/timeline"
)

// HandleMessage is the bridge message entry point. It ports
// “NpcAgentRuntime.handle_message“ including its dispatch table and exact
// reply payloads.
//
// A returned error means the runtime failed internally (for example the
// timeline could not be written); validation problems come back as
// “{"type": "error", "reason": ...}“ payloads, exactly like Python.
func (r *Runtime) HandleMessage(ctx context.Context, raw map[string]any) (map[string]any, error) {
	ctx = nonNilContext(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()

	messageType := strings.ToLower(domain.StringFrom(raw["type"]))
	switch messageType {
	case "world_update":
		return r.handleWorldUpdate(ctx, raw)
	case "ped_scan", "nearby_peds":
		return r.handlePedScan(ctx, raw)
	case "game_event", "event":
		return r.handleGameEvent(ctx, raw)
	case "action_result", "action_completed", "action_failed":
		return r.handleActionResult(ctx, raw)
	case "player_speech", "push_to_talk_transcript":
		return r.handlePlayerSpeech(ctx, raw)
	case "push_to_talk":
		return r.handlePushToTalk(ctx, raw)
	case "story_safety", "mission_state":
		return r.handleStorySafety(ctx, raw)
	case "hello", "bridge_hello":
		return map[string]any{"type": "hello_ack", "server_time": r.now()}, nil
	}
	return errorReply(fmt.Sprintf("unknown message type: %s", pyReprString(messageType))), nil
}

func (r *Runtime) handleWorldUpdate(ctx context.Context, raw map[string]any) (map[string]any, error) {
	worldState, err := r.normalizer.WorldStateFromMessage(raw)
	if err != nil {
		return errorReply(err.Error()), nil
	}
	r.world.Update(worldState)
	if err := r.maybeReleaseFromWorldState(ctx, worldState); err != nil {
		return nil, err
	}
	if err := r.maybeScheduleIdleThinking(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"type": "world_update_ack", "npc_id": worldState.NPCID}, nil
}

// maybeReleaseFromWorldState releases NPCs the player has walked away from
// based on a world update's player distance.
func (r *Runtime) maybeReleaseFromWorldState(ctx context.Context, worldState domain.WorldState) error {
	distance, present := worldState.Player["distance"]
	if !present || distance == nil {
		return nil
	}
	distanceM, ok := coerceFloat(distance)
	if !ok {
		return nil
	}
	candidateDistance := r.settings.Activation.CandidateDistanceM
	if distanceM <= candidateDistance {
		return nil
	}
	transition := r.ownership.UpdateFromScan(state.ScanUpdate{
		NPCID:              worldState.NPCID,
		DistanceM:          distanceM,
		Eligible:           true,
		CandidateDistanceM: candidateDistance,
		AwareDistanceM:     r.settings.Activation.AwareDistanceM,
	})
	if transition == nil {
		return nil
	}
	_, err := r.recordTransition(ctx, transition, nil, nil)
	return err
}

func (r *Runtime) handlePedScan(ctx context.Context, raw map[string]any) (map[string]any, error) {
	candidateDistance := r.settings.Activation.CandidateDistanceM
	awareDistance := r.settings.Activation.AwareDistanceM
	processed := 0
	transitions := []map[string]any{}

	peds, valid := anySlice(raw["peds"])
	if !valid {
		return errorReply("ped_scan.peds must be an array"), nil
	}
	trackedLimit := r.settings.Scheduler.NearbyTrackedLimit
	sort.SliceStable(peds, func(left, right int) bool {
		return pedDistanceKey(peds[left]) < pedDistanceKey(peds[right])
	})
	if trackedLimit < 0 {
		trackedLimit = 0
	}
	if len(peds) > trackedLimit {
		peds = peds[:trackedLimit]
	}

	for _, payload := range peds {
		pedPayload, ok := payload.(map[string]any)
		if !ok {
			continue
		}
		ped := domain.PedSnapshotFromMap(pedPayload)
		if ped.EntityID == "" {
			continue
		}
		r.lastPedScan[ped.EntityID] = ped
		distance := 0.0
		if ped.DistanceM != nil {
			distance = *ped.DistanceM
		}
		questTransition, err := r.handleQuestDialoguePed(ctx, ped, distance, candidateDistance, awareDistance)
		if err != nil {
			return nil, err
		}
		if questTransition != nil {
			transitions = append(transitions, questTransition)
			processed++
			continue
		}
		if r.agentStateLocked(ped.EntityID).DialogueOnly {
			// Quest dialogue overlay is active; Rockstar keeps all tasks.
			processed++
			continue
		}
		eligibility := r.eligibility.CanActivate(ped)
		transition := r.ownership.UpdateFromScan(state.ScanUpdate{
			NPCID:              ped.EntityID,
			DistanceM:          distance,
			Eligible:           eligibility.Eligible,
			CandidateDistanceM: candidateDistance,
			AwareDistanceM:     awareDistance,
		})
		if transition != nil {
			recorded, err := r.recordTransition(ctx, transition, &ped, eligibilityReasons(eligibility))
			if err != nil {
				return nil, err
			}
			transitions = append(transitions, recorded)
			if transition.To == ownershipAIActive {
				if _, err := r.scheduleReasoning(ctx, ped.EntityID, "ped_scan_activation", nil); err != nil {
					return nil, err
				}
			}
		}
		processed++
	}
	if err := r.maybeScheduleIdleThinking(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"type": "ped_scan_ack", "processed": processed, "transitions": transitions}, nil
}

func (r *Runtime) handleGameEvent(ctx context.Context, raw map[string]any) (map[string]any, error) {
	npcID := domain.StringFrom(raw["npc_id"])
	if npcID == "" {
		npcID = r.npcFromPending(raw)
	}
	if npcID == "" {
		return errorReply("game_event missing npc_id"), nil
	}
	seq, err := r.nextSeq(npcID)
	if err != nil {
		return nil, err
	}
	normalized, err := r.normalizer.EventFromMessage(raw, seq, npcID)
	if err != nil {
		return errorReply(err.Error()), nil
	}
	// Store.Append mirrors Python's ``timeline.append(event)``: the normalized
	// event is persisted as-is (including a bridge-supplied event_id), with the
	// sequence number repaired under the store lock.
	event, err := r.timeline.Append(normalized)
	if err != nil {
		return nil, err
	}
	r.world.ApplyEvent(event, worldRecentEventLimit)
	if err := r.reactToEvent(ctx, event); err != nil {
		return nil, err
	}
	return map[string]any{"type": "event_ack", "npc_id": npcID, "seq": event.Seq}, nil
}

func (r *Runtime) handleActionResult(ctx context.Context, raw map[string]any) (map[string]any, error) {
	request, found := r.dispatcher.Resolve(raw)
	npcID := domain.StringFrom(raw["npc_id"])
	if npcID == "" && found {
		npcID = request.NPCID
	}
	if npcID == "" {
		return errorReply("action_result missing npc_id and no pending request"), nil
	}
	payload := copyMap(raw)
	payload["npc_id"] = npcID
	eventPayload := r.normalizer.ActionResult(payload)
	seq, err := r.nextSeq(npcID)
	if err != nil {
		return nil, err
	}
	normalized, err := r.normalizer.EventFromMessage(eventPayload, seq, npcID)
	if err != nil {
		return errorReply(err.Error()), nil
	}
	// Store.Append mirrors Python's ``timeline.append(event)``: the normalized
	// event is persisted as-is (including a bridge-supplied event_id), with the
	// sequence number repaired under the store lock.
	event, err := r.timeline.Append(normalized)
	if err != nil {
		return nil, err
	}
	r.world.ApplyEvent(event, worldRecentEventLimit)
	if event.EventName == "ACTION_FAILED" {
		if _, err := r.scheduleReasoning(ctx, npcID, "action_failed", &event); err != nil {
			return nil, err
		}
	}
	return map[string]any{"type": "action_result_ack", "npc_id": npcID, "seq": event.Seq}, nil
}

func (r *Runtime) handlePlayerSpeech(ctx context.Context, raw map[string]any) (map[string]any, error) {
	npcID := domain.StringFrom(raw["npc_id"])
	if npcID == "" {
		npcID = domain.StringFrom(raw["target_npc_id"])
	}
	text := domain.StringFrom(raw["text"])
	if npcID == "" {
		return errorReply("player_speech missing npc_id"), nil
	}
	if text == "" {
		return errorReply("player_speech missing text"), nil
	}
	payload := r.normalizer.PlayerSpoke(npcID, text, nil)
	payload["timestamp"] = raw["timestamp"]
	payload["game_time"] = raw["game_time"]
	payload["location"] = raw["location"]
	return r.handleGameEvent(ctx, payload)
}

func (r *Runtime) handlePushToTalk(ctx context.Context, raw map[string]any) (map[string]any, error) {
	action := "start"
	if value, present := raw["action"]; present {
		action = strings.ToLower(pyStr(value))
	}
	npcID := domain.StringFrom(raw["npc_id"])
	if npcID == "" {
		npcID = domain.StringFrom(raw["target_npc_id"])
	}
	switch action {
	case "start":
		if npcID == "" {
			npcID = r.selectConversationTarget()
		}
		if npcID == "" {
			return map[string]any{"type": "push_to_talk", "target": nil, "reason": "no_valid_target"}, nil
		}
		st := r.agentStateLocked(npcID)
		if err := r.ensureSingleConversation(ctx, npcID); err != nil {
			return nil, err
		}
		if r.ownership.State(npcID) == ownershipAIConversation {
			// Barge-in: stop current TTS/attention and listen again.
			if _, err := r.appendEvent(npcID, "CONVERSATION_INTERRUPTED", timeline.AppendOptions{
				Data:       map[string]any{"by": "player"},
				Tags:       []string{"conversation"},
				Importance: float64Pointer(0.4),
			}); err != nil {
				return nil, err
			}
		} else {
			transition := r.ownership.PushToTalk(npcID)
			if transition != nil {
				if _, err := r.recordTransition(ctx, transition, nil, nil); err != nil {
					return nil, err
				}
			}
		}
		st.ConversationActive = true
		return map[string]any{"type": "push_to_talk", "action": "start", "target": npcID}, nil
	case "stop":
		if npcID != "" {
			transition := r.ownership.ConversationEnded(npcID)
			if transition != nil {
				if _, err := r.recordTransition(ctx, transition, nil, nil); err != nil {
					return nil, err
				}
			}
			r.agentStateLocked(npcID).ConversationActive = false
			if _, err := r.appendEvent(npcID, "CONVERSATION_ENDED", timeline.AppendOptions{
				Data: map[string]any{"by": "player"},
				Tags: []string{"conversation"},
			}); err != nil {
				return nil, err
			}
		}
		var target any
		if npcID != "" {
			target = npcID
		}
		return map[string]any{"type": "push_to_talk", "action": "stop", "target": target}, nil
	}
	return errorReply(fmt.Sprintf("unknown push_to_talk action: %s", pyReprString(action))), nil
}

// handleStorySafety suspends or resumes all non-Rockstar ownership for story
// safety. The bridge is expected to send this conservatively around missions,
// cutscenes, and other protected script states.
func (r *Runtime) handleStorySafety(ctx context.Context, raw map[string]any) (map[string]any, error) {
	active := false
	if value, present := raw["active"]; present {
		active = domain.BoolFrom(value)
	} else {
		active = domain.BoolFrom(raw["in_story"])
	}
	npcID := domain.StringFrom(raw["npc_id"])
	transitions := []map[string]any{}

	targets := []string{}
	if npcID != "" {
		targets = append(targets, npcID)
	} else {
		targets = r.ownership.OwnedNPCs()
		if len(targets) == 0 {
			targets = r.agentStateIDsLocked()
		}
	}

	for _, target := range targets {
		var transition *state.Transition
		if active {
			reason := "story_safety"
			if value, present := raw["reason"]; present {
				reason = domain.StringFrom(value)
			}
			transition = r.ownership.Suspend(target, reason)
		} else {
			reason := "story_safety_cleared"
			if value, present := raw["reason"]; present {
				reason = domain.StringFrom(value)
			}
			transition = r.ownership.Resume(target, reason)
		}
		if transition != nil {
			recorded, err := r.recordTransition(ctx, transition, nil, nil)
			if err != nil {
				return nil, err
			}
			transitions = append(transitions, recorded)
		}
	}
	return map[string]any{"type": "story_safety_ack", "active": active, "transitions": transitions}, nil
}

// worldRecentEventLimit mirrors WorldStateStore.apply_event's default
// “max_recent“.
const worldRecentEventLimit = 50

func errorReply(reason string) map[string]any {
	return map[string]any{"type": "error", "reason": reason}
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// pedDistanceKey mirrors the Python sort key:
// “float((p or {}).get("distance_m") or 0.0) if isinstance(p, dict) else 0.0“.
func pedDistanceKey(payload any) float64 {
	typed, ok := payload.(map[string]any)
	if !ok {
		return 0
	}
	distance, ok := coerceFloat(typed["distance_m"])
	if !ok {
		return 0
	}
	return distance
}

// anySlice mirrors Python's “raw.get("peds") or []“ followed by the
// “isinstance(peds, list)“ check.
func anySlice(value any) ([]any, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, true
	case []any:
		return typed, true
	case []map[string]any:
		converted := make([]any, 0, len(typed))
		for _, item := range typed {
			converted = append(converted, item)
		}
		return converted, true
	case string:
		if typed == "" {
			return nil, true
		}
	}
	return nil, false
}

// eligibilityReasons mirrors “EligibilityResult.reasons“.
func eligibilityReasons(result state.Result) []string {
	return result.Reasons()
}
