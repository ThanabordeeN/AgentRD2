package state

import (
	"testing"
)

// statePtr returns a pointer to a copy of state, for record fixtures.
func statePtr(state OwnershipState) *OwnershipState {
	return &state
}

// TestTransitionEventName covers every from/to pair, matching the Python
// "OwnershipTransition.event_name" property exactly (including that a
// SUSPENDED source always reports AGENT_RESUMED and that a RELEASE target
// always reports NPC_RELEASED).
func TestTransitionEventName(t *testing.T) {
	cases := []struct {
		from   OwnershipState
		to     OwnershipState
		want   string
		wantOK bool
	}{
		{from: StateRockstar, to: StateRockstar, want: "", wantOK: false},
		{from: StateRockstar, to: StateCandidate, want: "", wantOK: false},
		{from: StateRockstar, to: StateAware, want: "", wantOK: false},
		{from: StateRockstar, to: StateAIActive, want: "", wantOK: false},
		{from: StateRockstar, to: StateAIConversation, want: "", wantOK: false},
		{from: StateRockstar, to: StateQuestDialogue, want: "QUEST_DIALOGUE_ENTERED", wantOK: true},
		{from: StateRockstar, to: StateSuspended, want: "AGENT_SUSPENDED", wantOK: true},
		{from: StateRockstar, to: StateRelease, want: "NPC_RELEASED", wantOK: true},
		{from: StateCandidate, to: StateRockstar, want: "", wantOK: false},
		{from: StateCandidate, to: StateCandidate, want: "", wantOK: false},
		{from: StateCandidate, to: StateAware, want: "", wantOK: false},
		{from: StateCandidate, to: StateAIActive, want: "NPC_ACTIVATED", wantOK: true},
		{from: StateCandidate, to: StateAIConversation, want: "", wantOK: false},
		{from: StateCandidate, to: StateQuestDialogue, want: "QUEST_DIALOGUE_ENTERED", wantOK: true},
		{from: StateCandidate, to: StateSuspended, want: "AGENT_SUSPENDED", wantOK: true},
		{from: StateCandidate, to: StateRelease, want: "NPC_RELEASED", wantOK: true},
		{from: StateAware, to: StateRockstar, want: "", wantOK: false},
		{from: StateAware, to: StateCandidate, want: "", wantOK: false},
		{from: StateAware, to: StateAware, want: "", wantOK: false},
		{from: StateAware, to: StateAIActive, want: "NPC_ACTIVATED", wantOK: true},
		{from: StateAware, to: StateAIConversation, want: "", wantOK: false},
		{from: StateAware, to: StateQuestDialogue, want: "QUEST_DIALOGUE_ENTERED", wantOK: true},
		{from: StateAware, to: StateSuspended, want: "AGENT_SUSPENDED", wantOK: true},
		{from: StateAware, to: StateRelease, want: "NPC_RELEASED", wantOK: true},
		{from: StateAIActive, to: StateRockstar, want: "", wantOK: false},
		{from: StateAIActive, to: StateCandidate, want: "", wantOK: false},
		{from: StateAIActive, to: StateAware, want: "", wantOK: false},
		{from: StateAIActive, to: StateAIActive, want: "", wantOK: false},
		{from: StateAIActive, to: StateAIConversation, want: "", wantOK: false},
		{from: StateAIActive, to: StateQuestDialogue, want: "QUEST_DIALOGUE_ENTERED", wantOK: true},
		{from: StateAIActive, to: StateSuspended, want: "AGENT_SUSPENDED", wantOK: true},
		{from: StateAIActive, to: StateRelease, want: "NPC_RELEASED", wantOK: true},
		{from: StateAIConversation, to: StateRockstar, want: "", wantOK: false},
		{from: StateAIConversation, to: StateCandidate, want: "", wantOK: false},
		{from: StateAIConversation, to: StateAware, want: "", wantOK: false},
		{from: StateAIConversation, to: StateAIActive, want: "", wantOK: false},
		{from: StateAIConversation, to: StateAIConversation, want: "", wantOK: false},
		{from: StateAIConversation, to: StateQuestDialogue, want: "QUEST_DIALOGUE_ENTERED", wantOK: true},
		{from: StateAIConversation, to: StateSuspended, want: "AGENT_SUSPENDED", wantOK: true},
		{from: StateAIConversation, to: StateRelease, want: "NPC_RELEASED", wantOK: true},
		{from: StateQuestDialogue, to: StateRockstar, want: "", wantOK: false},
		{from: StateQuestDialogue, to: StateCandidate, want: "", wantOK: false},
		{from: StateQuestDialogue, to: StateAware, want: "", wantOK: false},
		{from: StateQuestDialogue, to: StateAIActive, want: "", wantOK: false},
		{from: StateQuestDialogue, to: StateAIConversation, want: "", wantOK: false},
		{from: StateQuestDialogue, to: StateQuestDialogue, want: "QUEST_DIALOGUE_ENTERED", wantOK: true},
		{from: StateQuestDialogue, to: StateSuspended, want: "AGENT_SUSPENDED", wantOK: true},
		{from: StateQuestDialogue, to: StateRelease, want: "NPC_RELEASED", wantOK: true},
		{from: StateSuspended, to: StateRockstar, want: "AGENT_RESUMED", wantOK: true},
		{from: StateSuspended, to: StateCandidate, want: "AGENT_RESUMED", wantOK: true},
		{from: StateSuspended, to: StateAware, want: "AGENT_RESUMED", wantOK: true},
		{from: StateSuspended, to: StateAIActive, want: "AGENT_RESUMED", wantOK: true},
		{from: StateSuspended, to: StateAIConversation, want: "AGENT_RESUMED", wantOK: true},
		{from: StateSuspended, to: StateQuestDialogue, want: "AGENT_RESUMED", wantOK: true},
		{from: StateSuspended, to: StateSuspended, want: "AGENT_RESUMED", wantOK: true},
		{from: StateSuspended, to: StateRelease, want: "AGENT_RESUMED", wantOK: true},
		{from: StateRelease, to: StateRockstar, want: "", wantOK: false},
		{from: StateRelease, to: StateCandidate, want: "", wantOK: false},
		{from: StateRelease, to: StateAware, want: "", wantOK: false},
		{from: StateRelease, to: StateAIActive, want: "", wantOK: false},
		{from: StateRelease, to: StateAIConversation, want: "", wantOK: false},
		{from: StateRelease, to: StateQuestDialogue, want: "QUEST_DIALOGUE_ENTERED", wantOK: true},
		{from: StateRelease, to: StateSuspended, want: "AGENT_SUSPENDED", wantOK: true},
		{from: StateRelease, to: StateRelease, want: "NPC_RELEASED", wantOK: true},
	}

	if len(cases) != len(allOwnershipStates())*len(allOwnershipStates()) {
		t.Fatalf("table covers %d pairs, want %d", len(cases), len(allOwnershipStates())*len(allOwnershipStates()))
	}
	for _, testCase := range cases {
		transition := Transition{NPCID: "npc_1", From: testCase.from, To: testCase.to, Reason: "r"}
		got, ok := transition.EventName()
		if got != testCase.want || ok != testCase.wantOK {
			t.Errorf("EventName(%s -> %s) = (%q, %v), want (%q, %v)",
				testCase.from, testCase.to, got, ok, testCase.want, testCase.wantOK)
		}
	}
}

func allOwnershipStates() []OwnershipState {
	return []OwnershipState{
		StateRockstar,
		StateCandidate,
		StateAware,
		StateAIActive,
		StateAIConversation,
		StateQuestDialogue,
		StateSuspended,
		StateRelease,
	}
}

// TestManagerFullFlow mirrors tests/test_ownership.py::test_full_flow.
func TestManagerFullFlow(t *testing.T) {
	manager := NewManager()

	steps := []struct {
		name       string
		update     ScanUpdate
		wantTo     OwnershipState
		wantReason string
	}{
		{"candidate", ScanUpdate{NPCID: "npc_1", DistanceM: 15, Eligible: true}, StateCandidate, "player_nearby"},
		{"aware", ScanUpdate{NPCID: "npc_1", DistanceM: 8, Eligible: true}, StateAware, "eligibility_passed"},
		{"active", ScanUpdate{NPCID: "npc_1", DistanceM: 8, Eligible: true, MeaningfulTrigger: true}, StateAIActive, "meaningful_trigger"},
	}
	for _, step := range steps {
		transition := manager.UpdateFromScan(step.update)
		if transition == nil {
			t.Fatalf("%s: got nil transition, want %s", step.name, step.wantTo)
		}
		if transition.To != step.wantTo || transition.Reason != step.wantReason {
			t.Fatalf("%s: got (%s, %q), want (%s, %q)",
				step.name, transition.To, transition.Reason, step.wantTo, step.wantReason)
		}
		if transition.From == transition.To {
			t.Fatalf("%s: From and To are both %s", step.name, transition.To)
		}
	}

	pushed := manager.PushToTalk("npc_1")
	if pushed == nil || pushed.To != StateAIConversation || pushed.Reason != "push_to_talk" {
		t.Fatalf("PushToTalk = %+v, want AI_CONVERSATION/push_to_talk", pushed)
	}
	ended := manager.ConversationEnded("npc_1")
	if ended == nil || ended.To != StateAIActive || ended.Reason != "conversation_ended" {
		t.Fatalf("ConversationEnded = %+v, want AI_ACTIVE/conversation_ended", ended)
	}
	released := manager.UpdateFromScan(ScanUpdate{NPCID: "npc_1", DistanceM: 50, Eligible: true})
	if released == nil || released.To != StateRelease || released.Reason != "player_left_area" {
		t.Fatalf("far scan = %+v, want RELEASE/player_left_area", released)
	}
	finalized := manager.FinalizeRelease("npc_1")
	if finalized == nil || finalized.To != StateRockstar || finalized.Reason != "release_complete" {
		t.Fatalf("FinalizeRelease = %+v, want ROCKSTAR/release_complete", finalized)
	}
	if got := manager.State("npc_1"); got != StateRockstar {
		t.Fatalf("State = %s, want ROCKSTAR", got)
	}
}

// TestManagerEligibilityFailureReleasesActiveAgent mirrors
// tests/test_ownership.py::test_eligibility_failure_releases_active_agent.
func TestManagerEligibilityFailureReleasesActiveAgent(t *testing.T) {
	manager := NewManager()
	manager.UpdateFromScan(ScanUpdate{NPCID: "npc_1", DistanceM: 15, Eligible: true})
	manager.UpdateFromScan(ScanUpdate{NPCID: "npc_1", DistanceM: 8, Eligible: true})
	manager.UpdateFromScan(ScanUpdate{NPCID: "npc_1", DistanceM: 8, Eligible: true, MeaningfulTrigger: true})

	transition := manager.UpdateFromScan(ScanUpdate{NPCID: "npc_1", DistanceM: 8, Eligible: false})
	if transition == nil {
		t.Fatal("got nil transition, want RELEASE")
	}
	if transition.To != StateRelease || transition.Reason != "eligibility_failed" {
		t.Fatalf("got (%s, %q), want (RELEASE, eligibility_failed)", transition.To, transition.Reason)
	}
}

// TestManagerSuspendAndResume mirrors
// tests/test_ownership.py::test_suspend_and_resume.
func TestManagerSuspendAndResume(t *testing.T) {
	manager := NewManager()
	manager.UpdateFromScan(ScanUpdate{NPCID: "npc_1", DistanceM: 8, Eligible: true})
	manager.UpdateFromScan(ScanUpdate{NPCID: "npc_1", DistanceM: 8, Eligible: true})
	manager.UpdateFromScan(ScanUpdate{NPCID: "npc_1", DistanceM: 8, Eligible: true, MeaningfulTrigger: true})

	suspended := manager.Suspend("npc_1", "mission")
	if suspended == nil || suspended.To != StateSuspended || suspended.Reason != "mission" {
		t.Fatalf("Suspend = %+v, want SUSPENDED/mission", suspended)
	}
	if record := manager.Get("npc_1"); record.PreviousState == nil || *record.PreviousState != StateAIActive {
		t.Fatalf("PreviousState = %v, want AI_ACTIVE", record.PreviousState)
	}
	resumed := manager.Resume("npc_1", "")
	if resumed == nil || resumed.To != StateAIActive || resumed.Reason != DefaultResumeReason {
		t.Fatalf("Resume = %+v, want AI_ACTIVE/%s", resumed, DefaultResumeReason)
	}
	if record := manager.Get("npc_1"); record.PreviousState != nil {
		t.Fatalf("PreviousState = %v, want nil after resume", *record.PreviousState)
	}
	if again := manager.Resume("npc_1", ""); again != nil {
		t.Fatalf("second Resume = %+v, want nil", again)
	}
}

// TestUpdateFromScanBranches is the heart of the ownership machine.  Each case
// starts from an explicit record state and applies one scan.
func TestUpdateFromScanBranches(t *testing.T) {
	cases := []struct {
		name       string
		from       OwnershipState
		update     ScanUpdate
		wantNil    bool
		wantTo     OwnershipState
		wantReason string
	}{
		{
			name:    "suspended ignores an eligible nearby scan",
			from:    StateSuspended,
			update:  ScanUpdate{NPCID: "npc_1", DistanceM: 1, Eligible: true, MeaningfulTrigger: true},
			wantNil: true,
		},
		{
			name:    "suspended ignores a failed eligibility scan",
			from:    StateSuspended,
			update:  ScanUpdate{NPCID: "npc_1", DistanceM: 40, Eligible: false},
			wantNil: true,
		},
		{
			name:    "rockstar stays put when eligibility fails",
			from:    StateRockstar,
			update:  ScanUpdate{NPCID: "npc_1", DistanceM: 5, Eligible: false},
			wantNil: true,
		},
		{
			name:    "rockstar far away stays put",
			from:    StateRockstar,
			update:  ScanUpdate{NPCID: "npc_1", DistanceM: 40, Eligible: true},
			wantNil: true,
		},
		{
			name:       "rockstar nearby becomes candidate",
			from:       StateRockstar,
			update:     ScanUpdate{NPCID: "npc_1", DistanceM: 20, Eligible: true},
			wantTo:     StateCandidate,
			wantReason: "player_nearby",
		},
		{
			name:       "release nearby re-candidates",
			from:       StateRelease,
			update:     ScanUpdate{NPCID: "npc_1", DistanceM: 5, Eligible: true},
			wantTo:     StateCandidate,
			wantReason: "player_nearby",
		},
		{
			name:       "release far away completes the hand-off",
			from:       StateRelease,
			update:     ScanUpdate{NPCID: "npc_1", DistanceM: 40, Eligible: true},
			wantTo:     StateRockstar,
			wantReason: "release_complete",
		},
		{
			name:       "release with failed eligibility completes the hand-off",
			from:       StateRelease,
			update:     ScanUpdate{NPCID: "npc_1", DistanceM: 5, Eligible: false},
			wantTo:     StateRockstar,
			wantReason: "release_complete",
		},
		{
			name:       "candidate inside the aware radius becomes aware",
			from:       StateCandidate,
			update:     ScanUpdate{NPCID: "npc_1", DistanceM: 10, Eligible: true},
			wantTo:     StateAware,
			wantReason: "eligibility_passed",
		},
		{
			name:    "candidate between aware and candidate radius stays put",
			from:    StateCandidate,
			update:  ScanUpdate{NPCID: "npc_1", DistanceM: 15, Eligible: true},
			wantNil: true,
		},
		{
			name:       "candidate beyond candidate radius returns to rockstar",
			from:       StateCandidate,
			update:     ScanUpdate{NPCID: "npc_1", DistanceM: 21, Eligible: true},
			wantTo:     StateRockstar,
			wantReason: "candidate_left_area",
		},
		{
			name:       "candidate with failed eligibility is released",
			from:       StateCandidate,
			update:     ScanUpdate{NPCID: "npc_1", DistanceM: 5, Eligible: false},
			wantTo:     StateRelease,
			wantReason: "eligibility_failed",
		},
		{
			name:       "aware with a meaningful trigger activates",
			from:       StateAware,
			update:     ScanUpdate{NPCID: "npc_1", DistanceM: 6, Eligible: true, MeaningfulTrigger: true},
			wantTo:     StateAIActive,
			wantReason: "meaningful_trigger",
		},
		{
			name:    "aware without a trigger stays put",
			from:    StateAware,
			update:  ScanUpdate{NPCID: "npc_1", DistanceM: 6, Eligible: true},
			wantNil: true,
		},
		{
			name:       "aware with a trigger but failed eligibility is not activated",
			from:       StateAware,
			update:     ScanUpdate{NPCID: "npc_1", DistanceM: 6, Eligible: false, MeaningfulTrigger: true},
			wantTo:     StateRelease,
			wantReason: "eligibility_failed",
		},
		{
			name:       "aware beyond candidate radius is released",
			from:       StateAware,
			update:     ScanUpdate{NPCID: "npc_1", DistanceM: 25, Eligible: true},
			wantTo:     StateRelease,
			wantReason: "player_left_area",
		},
		{
			name:       "active beyond candidate radius is released",
			from:       StateAIActive,
			update:     ScanUpdate{NPCID: "npc_1", DistanceM: 25, Eligible: true, MeaningfulTrigger: true},
			wantTo:     StateRelease,
			wantReason: "player_left_area",
		},
		{
			name:    "active nearby stays put",
			from:    StateAIActive,
			update:  ScanUpdate{NPCID: "npc_1", DistanceM: 5, Eligible: true},
			wantNil: true,
		},
		{
			name:       "active with failed eligibility is released",
			from:       StateAIActive,
			update:     ScanUpdate{NPCID: "npc_1", DistanceM: 5, Eligible: false},
			wantTo:     StateRelease,
			wantReason: "eligibility_failed",
		},
		{
			name:       "conversation beyond candidate radius is released",
			from:       StateAIConversation,
			update:     ScanUpdate{NPCID: "npc_1", DistanceM: 25, Eligible: true},
			wantTo:     StateRelease,
			wantReason: "player_left_area",
		},
		{
			name:    "conversation nearby stays put",
			from:    StateAIConversation,
			update:  ScanUpdate{NPCID: "npc_1", DistanceM: 5, Eligible: true},
			wantNil: true,
		},
		{
			name:       "quest dialogue with failed eligibility is released",
			from:       StateQuestDialogue,
			update:     ScanUpdate{NPCID: "npc_1", DistanceM: 5, Eligible: false},
			wantTo:     StateRelease,
			wantReason: "eligibility_failed",
		},
		{
			name:       "quest dialogue beyond candidate radius is released",
			from:       StateQuestDialogue,
			update:     ScanUpdate{NPCID: "npc_1", DistanceM: 25, Eligible: true},
			wantTo:     StateRelease,
			wantReason: "player_left_area",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			manager := NewManager()
			manager.Get("npc_1").State = testCase.from
			update := testCase.update
			update.NPCID = "npc_1"

			transition := manager.UpdateFromScan(update)
			if testCase.wantNil {
				if transition != nil {
					t.Fatalf("got %+v, want nil", transition)
				}
				if got := manager.State("npc_1"); got != testCase.from {
					t.Fatalf("state = %s, want unchanged %s", got, testCase.from)
				}
				return
			}
			if transition == nil {
				t.Fatalf("got nil transition, want %s/%s", testCase.wantTo, testCase.wantReason)
			}
			if transition.To != testCase.wantTo || transition.Reason != testCase.wantReason {
				t.Fatalf("got (%s, %q), want (%s, %q)",
					transition.To, transition.Reason, testCase.wantTo, testCase.wantReason)
			}
			if transition.From != testCase.from {
				t.Fatalf("From = %s, want %s", transition.From, testCase.from)
			}
		})
	}
}

// TestUpdateFromScanDistances checks the custom radii and the zero-value
// fallbacks to the Python defaults.
func TestUpdateFromScanDistances(t *testing.T) {
	cases := []struct {
		name       string
		distanceM  float64
		candidateM float64
		awareM     float64
		wantTo     OwnershipState
		wantReason string
		wantNil    bool
	}{
		{
			name:       "zero radii fall back to the python defaults",
			distanceM:  15,
			wantTo:     StateCandidate,
			wantReason: "player_nearby",
		},
		{
			name:       "zero radii fall back to the python defaults at the aware radius",
			distanceM:  10,
			wantTo:     StateCandidate,
			wantReason: "player_nearby",
		},
		{
			name:       "custom candidate radius stretches the ring",
			distanceM:  40,
			candidateM: 50,
			awareM:     30,
			wantTo:     StateCandidate,
			wantReason: "player_nearby",
		},
		{
			name:       "custom candidate radius leaves a distant ped alone",
			distanceM:  40,
			candidateM: 30,
			awareM:     10,
			wantNil:    true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			manager := NewManager()
			transition := manager.UpdateFromScan(ScanUpdate{
				NPCID:              "npc_1",
				DistanceM:          testCase.distanceM,
				Eligible:           true,
				CandidateDistanceM: testCase.candidateM,
				AwareDistanceM:     testCase.awareM,
			})
			if testCase.wantNil {
				if transition != nil {
					t.Fatalf("got %+v, want nil", transition)
				}
				return
			}
			if transition == nil || transition.To != testCase.wantTo || transition.Reason != testCase.wantReason {
				t.Fatalf("got %+v, want %s/%s", transition, testCase.wantTo, testCase.wantReason)
			}
		})
	}
}

// TestEnterQuestDialogue covers every source state.
func TestEnterQuestDialogue(t *testing.T) {
	cases := []struct {
		from    OwnershipState
		wantNil bool
	}{
		{from: StateRockstar},
		{from: StateCandidate},
		{from: StateAware},
		{from: StateAIActive},
		{from: StateAIConversation},
		{from: StateQuestDialogue, wantNil: true},
		{from: StateSuspended, wantNil: true},
		{from: StateRelease, wantNil: true},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.from), func(t *testing.T) {
			manager := NewManager()
			manager.Get("npc_1").State = testCase.from
			transition := manager.EnterQuestDialogue("npc_1")
			if testCase.wantNil {
				if transition != nil {
					t.Fatalf("got %+v, want nil", transition)
				}
				return
			}
			if transition == nil || transition.To != StateQuestDialogue || transition.Reason != "quest_dialogue" {
				t.Fatalf("got %+v, want QUEST_DIALOGUE/quest_dialogue", transition)
			}
			if event, ok := transition.EventName(); !ok || event != "QUEST_DIALOGUE_ENTERED" {
				t.Fatalf("EventName = (%q, %v), want QUEST_DIALOGUE_ENTERED", event, ok)
			}
		})
	}
}

// TestPushToTalkAndConversationEnded covers every source state.
func TestPushToTalkAndConversationEnded(t *testing.T) {
	pushCases := []struct {
		from    OwnershipState
		wantNil bool
	}{
		{from: StateRockstar, wantNil: true},
		{from: StateCandidate, wantNil: true},
		{from: StateAware},
		{from: StateAIActive},
		{from: StateAIConversation},
		{from: StateQuestDialogue},
		{from: StateSuspended, wantNil: true},
		{from: StateRelease, wantNil: true},
	}
	for _, testCase := range pushCases {
		t.Run("push/"+string(testCase.from), func(t *testing.T) {
			manager := NewManager()
			manager.Get("npc_1").State = testCase.from
			transition := manager.PushToTalk("npc_1")
			if testCase.wantNil {
				if transition != nil {
					t.Fatalf("got %+v, want nil", transition)
				}
				return
			}
			if transition == nil || transition.To != StateAIConversation || transition.Reason != "push_to_talk" {
				t.Fatalf("got %+v, want AI_CONVERSATION/push_to_talk", transition)
			}
		})
	}

	endCases := []struct {
		from    OwnershipState
		wantNil bool
	}{
		{from: StateRockstar, wantNil: true},
		{from: StateCandidate, wantNil: true},
		{from: StateAware, wantNil: true},
		{from: StateAIActive, wantNil: true},
		{from: StateAIConversation},
		{from: StateQuestDialogue, wantNil: true},
		{from: StateSuspended, wantNil: true},
		{from: StateRelease, wantNil: true},
	}
	for _, testCase := range endCases {
		t.Run("end/"+string(testCase.from), func(t *testing.T) {
			manager := NewManager()
			manager.Get("npc_1").State = testCase.from
			transition := manager.ConversationEnded("npc_1")
			if testCase.wantNil {
				if transition != nil {
					t.Fatalf("got %+v, want nil", transition)
				}
				return
			}
			if transition == nil || transition.To != StateAIActive || transition.Reason != "conversation_ended" {
				t.Fatalf("got %+v, want AI_ACTIVE/conversation_ended", transition)
			}
		})
	}
}

// TestRequestReleaseAndFinalize covers the release hand-off.
func TestRequestReleaseAndFinalize(t *testing.T) {
	cases := []struct {
		name       string
		from       OwnershipState
		reason     string
		wantNil    bool
		wantState  OwnershipState
		wantReason string
	}{
		{name: "default reason", from: StateRockstar, wantState: StateRelease, wantReason: DefaultReleaseReason},
		{name: "explicit reason", from: StateAIActive, reason: "player_hostile", wantState: StateRelease, wantReason: "player_hostile"},
		{name: "suspended can be released", from: StateSuspended, wantState: StateRelease, wantReason: DefaultReleaseReason},
		{name: "already releasing", from: StateRelease, wantNil: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			manager := NewManager()
			manager.Get("npc_1").State = testCase.from
			transition := manager.RequestRelease("npc_1", testCase.reason)
			if testCase.wantNil {
				if transition != nil {
					t.Fatalf("got %+v, want nil", transition)
				}
				return
			}
			if transition == nil || transition.To != testCase.wantState || transition.Reason != testCase.wantReason {
				t.Fatalf("got %+v, want %s/%s", transition, testCase.wantState, testCase.wantReason)
			}
		})
	}

	manager := NewManager()
	manager.Get("npc_1").State = StateRockstar
	if transition := manager.FinalizeRelease("npc_1"); transition != nil {
		t.Fatalf("FinalizeRelease from ROCKSTAR = %+v, want nil", transition)
	}
	manager.Get("npc_1").State = StateRelease
	transition := manager.FinalizeRelease("npc_1")
	if transition == nil || transition.To != StateRockstar || transition.Reason != "release_complete" {
		t.Fatalf("FinalizeRelease = %+v, want ROCKSTAR/release_complete", transition)
	}
}

// TestSuspendResumeBranches covers every suspend/resume source state and the
// AWARE fallback when the remembered state is unusable.
func TestSuspendResumeBranches(t *testing.T) {
	suspendCases := []struct {
		from    OwnershipState
		wantNil bool
	}{
		{from: StateRockstar, wantNil: true},
		{from: StateCandidate},
		{from: StateAware},
		{from: StateAIActive},
		{from: StateAIConversation},
		{from: StateQuestDialogue},
		{from: StateSuspended, wantNil: true},
		{from: StateRelease},
	}
	for _, testCase := range suspendCases {
		t.Run("suspend/"+string(testCase.from), func(t *testing.T) {
			manager := NewManager()
			manager.Get("npc_1").State = testCase.from
			transition := manager.Suspend("npc_1", "")
			if testCase.wantNil {
				if transition != nil {
					t.Fatalf("got %+v, want nil", transition)
				}
				if record := manager.Get("npc_1"); record.PreviousState != nil {
					t.Fatalf("PreviousState = %v, want nil", *record.PreviousState)
				}
				return
			}
			if transition == nil || transition.To != StateSuspended || transition.Reason != DefaultSuspendReason {
				t.Fatalf("got %+v, want SUSPENDED/%s", transition, DefaultSuspendReason)
			}
			if event, ok := transition.EventName(); !ok || event != "AGENT_SUSPENDED" {
				t.Fatalf("EventName = (%q, %v), want AGENT_SUSPENDED", event, ok)
			}
			record := manager.Get("npc_1")
			if record.PreviousState == nil || *record.PreviousState != testCase.from {
				t.Fatalf("PreviousState = %v, want %s", record.PreviousState, testCase.from)
			}
		})
	}

	resumeCases := []struct {
		name     string
		previous *OwnershipState
		want     OwnershipState
	}{
		{name: "restores the previous state", previous: statePtr(StateAIActive), want: StateAIActive},
		{name: "restores a candidate", previous: statePtr(StateCandidate), want: StateCandidate},
		{name: "rockstar is not usable", previous: statePtr(StateRockstar), want: StateAware},
		{name: "release is not usable", previous: statePtr(StateRelease), want: StateAware},
		{name: "suspended is not usable", previous: statePtr(StateSuspended), want: StateAware},
		{name: "no previous state falls back to aware", previous: nil, want: StateAware},
	}
	for _, testCase := range resumeCases {
		t.Run(testCase.name, func(t *testing.T) {
			manager := NewManager()
			record := manager.Get("npc_1")
			record.State = StateSuspended
			record.PreviousState = testCase.previous

			transition := manager.Resume("npc_1", "")
			if transition == nil || transition.To != testCase.want || transition.Reason != DefaultResumeReason {
				t.Fatalf("got %+v, want %s/%s", transition, testCase.want, DefaultResumeReason)
			}
			if event, ok := transition.EventName(); !ok || event != "AGENT_RESUMED" {
				t.Fatalf("EventName = (%q, %v), want AGENT_RESUMED", event, ok)
			}
			if record.PreviousState != nil {
				t.Fatalf("PreviousState = %v, want nil", *record.PreviousState)
			}
		})
	}

	t.Run("resume outside suspended is a no-op", func(t *testing.T) {
		manager := NewManager()
		manager.Get("npc_1").State = StateAIActive
		if transition := manager.Resume("npc_1", ""); transition != nil {
			t.Fatalf("got %+v, want nil", transition)
		}
	})
}

// TestCanReasonCanSpeak covers every state.
func TestCanReasonCanSpeak(t *testing.T) {
	cases := []struct {
		state         OwnershipState
		wantCanReason bool
		wantCanSpeak  bool
	}{
		{StateRockstar, false, false},
		{StateCandidate, false, false},
		{StateAware, true, false},
		{StateAIActive, true, true},
		{StateAIConversation, true, true},
		{StateQuestDialogue, true, true},
		{StateSuspended, false, false},
		{StateRelease, false, false},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.state), func(t *testing.T) {
			manager := NewManager()
			manager.Get("npc_1").State = testCase.state
			if got := manager.CanReason("npc_1"); got != testCase.wantCanReason {
				t.Errorf("CanReason = %v, want %v", got, testCase.wantCanReason)
			}
			if got := manager.CanSpeak("npc_1"); got != testCase.wantCanSpeak {
				t.Errorf("CanSpeak = %v, want %v", got, testCase.wantCanSpeak)
			}
		})
	}
}

// TestManagerRecordBookkeeping checks the record defaults, history and the
// insertion-ordered OwnedNPCs view.
func TestManagerRecordBookkeeping(t *testing.T) {
	manager := NewManager()
	if owned := manager.OwnedNPCs(); len(owned) != 0 {
		t.Fatalf("OwnedNPCs = %v, want empty", owned)
	}

	record := manager.Get("npc_1")
	if record.NPCID != "npc_1" || record.State != StateRockstar {
		t.Fatalf("record = %+v, want npc_1/ROCKSTAR", record)
	}
	if record.PreviousState != nil || len(record.History) != 0 {
		t.Fatalf("record = %+v, want empty history and previous state", record)
	}
	if manager.State("npc_2") != StateRockstar {
		t.Fatalf("State(npc_2) = %s, want ROCKSTAR", manager.State("npc_2"))
	}
	manager.Get("npc_3")

	owned := manager.OwnedNPCs()
	want := []string{"npc_1", "npc_2", "npc_3"}
	if len(owned) != len(want) {
		t.Fatalf("OwnedNPCs = %v, want %v", owned, want)
	}
	for index, npcID := range want {
		if owned[index] != npcID {
			t.Fatalf("OwnedNPCs = %v, want %v", owned, want)
		}
	}
	owned[0] = "mutated"
	if manager.OwnedNPCs()[0] != "npc_1" {
		t.Fatal("OwnedNPCs must return a copy")
	}

	manager.Get("npc_1").State = StateAware
	manager.Suspend("npc_1", "")
	record = manager.Get("npc_1")
	if len(record.History) != 1 {
		t.Fatalf("history length = %d, want 1", len(record.History))
	}
	applied := record.History[0]
	if applied.NPCID != "npc_1" || applied.From != StateAware || applied.To != StateSuspended {
		t.Fatalf("history[0] = %+v", applied)
	}
	if applied.Reason != DefaultSuspendReason || applied.Timestamp <= 0 {
		t.Fatalf("history[0] = %+v, want reason and timestamp", applied)
	}
}

// TestManagerZeroValue exercises the zero Manager, which must behave like an
// empty manager rather than panicking.
func TestManagerZeroValue(t *testing.T) {
	var manager Manager
	if got := manager.State("npc_1"); got != StateRockstar {
		t.Fatalf("State = %s, want ROCKSTAR", got)
	}
	transition := manager.UpdateFromScan(ScanUpdate{NPCID: "npc_1", DistanceM: 5, Eligible: true})
	if transition == nil || transition.To != StateCandidate {
		t.Fatalf("transition = %+v, want CANDIDATE", transition)
	}
}

// TestScanUpdateZeroDistances documents the Go stand-in for the Python
// keyword defaults.
func TestScanUpdateZeroDistances(t *testing.T) {
	if DefaultCandidateDistanceM != 20.0 || DefaultAwareDistanceM != 10.0 {
		t.Fatalf("defaults = (%v, %v), want (20, 10)", DefaultCandidateDistanceM, DefaultAwareDistanceM)
	}
	manager := NewManager()
	manager.UpdateFromScan(ScanUpdate{NPCID: "npc_1", DistanceM: 5, Eligible: true})
	if got := manager.State("npc_1"); got != StateCandidate {
		t.Fatalf("State = %s, want CANDIDATE", got)
	}
	// 11m is outside the 10m aware radius but inside the 20m candidate radius.
	manager.UpdateFromScan(ScanUpdate{NPCID: "npc_1", DistanceM: 11, Eligible: true})
	if got := manager.State("npc_1"); got != StateCandidate {
		t.Fatalf("State = %s, want CANDIDATE", got)
	}
	manager.UpdateFromScan(ScanUpdate{NPCID: "npc_1", DistanceM: 9, Eligible: true})
	if got := manager.State("npc_1"); got != StateAware {
		t.Fatalf("State = %s, want AWARE", got)
	}
}

// TestTransitionShape checks the fields the agent layer records for a
// transition.
func TestTransitionShape(t *testing.T) {
	manager := NewManager()
	transition := manager.UpdateFromScan(ScanUpdate{NPCID: "npc_1", DistanceM: 5, Eligible: true})
	if transition == nil {
		t.Fatal("got nil transition")
	}
	if transition.NPCID != "npc_1" || transition.Timestamp <= 0 {
		t.Fatalf("transition = %+v", transition)
	}
	if transition.From != StateRockstar || transition.To != StateCandidate {
		t.Fatalf("transition = %+v, want ROCKSTAR -> CANDIDATE", transition)
	}
	if _, ok := transition.EventName(); ok {
		t.Fatal("ROCKSTAR -> CANDIDATE must not be a notable event")
	}
}
