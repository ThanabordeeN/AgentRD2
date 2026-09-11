package agent

import "github.com/ThanabordeeN/AgentRD2/runtime-go/internal/state"

// This file is the single point of contact between the agent runtime and the
// ownership state machine. Keeping the state constants here means the rest of
// the package reads like the Python original
// (“OwnershipState.AI_ACTIVE“) without repeating the package qualifier.
const (
	ownershipRockstar       = state.StateRockstar
	ownershipCandidate      = state.StateCandidate
	ownershipAware          = state.StateAware
	ownershipAIActive       = state.StateAIActive
	ownershipAIConversation = state.StateAIConversation
	ownershipQuestDialogue  = state.StateQuestDialogue
	ownershipSuspended      = state.StateSuspended
	ownershipRelease        = state.StateRelease
)

// ownershipStateName mirrors “OwnershipState.value“: the uppercase wire name
// recorded in timeline events and agent context flags.
func ownershipStateName(value state.OwnershipState) string {
	return string(value)
}
