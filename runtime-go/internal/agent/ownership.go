package agent

import "github.com/ThanabordeeN/AgentRD2/runtime-go/internal/state"

// This file is the single point of contact between the agent runtime and the
// ownership state machine. Keeping the state constants and transition access
// here means the rest of the package reads like the Python original
// (``OwnershipState.AI_ACTIVE``) without repeating the package qualifier.
const (
	ownershipRockstar       = state.Rockstar
	ownershipCandidate      = state.Candidate
	ownershipAware          = state.Aware
	ownershipAIActive       = state.AIActive
	ownershipAIConversation = state.AIConversation
	ownershipQuestDialogue  = state.QuestDialogue
	ownershipSuspended      = state.Suspended
	ownershipRelease        = state.Release
)

// ownershipStateName mirrors ``OwnershipState.value``: the uppercase wire name
// recorded in timeline events and agent context flags.
func ownershipStateName(value state.OwnershipState) string {
	return string(value)
}

// transitionEventName mirrors the Python ``OwnershipTransition.event_name``
// property.
//
// Resume/suspend and release are checked before activation so a suspended ->
// AI_ACTIVE resume is not mislabeled as a new activation.
func transitionEventName(from, to state.OwnershipState) string {
	switch {
	case from == ownershipSuspended:
		return "AGENT_RESUMED"
	case to == ownershipQuestDialogue:
		return "QUEST_DIALOGUE_ENTERED"
	case to == ownershipSuspended:
		return "AGENT_SUSPENDED"
	case to == ownershipRelease:
		return "NPC_RELEASED"
	case to == ownershipAIActive && (from == ownershipCandidate || from == ownershipAware):
		return "NPC_ACTIVATED"
	}
	return ""
}
