package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

// boolPtr returns a pointer to a copy of value, for ped fixtures.
func boolPtr(value bool) *bool { return &value }

// safePed mirrors tests/test_eligibility.py::safe_ped: every safety field is
// explicitly confirmed, so only the rule under test can reject it.
func safePed() domain.PedSnapshot {
	return domain.PedSnapshot{
		EntityID:         "npc_1",
		Model:            "a_m_m_farmer_01",
		Name:             "Elias Carter",
		IsPed:            boolPtr(true),
		IsHuman:          boolPtr(true),
		IsAlive:          boolPtr(true),
		IsPlayer:         boolPtr(false),
		IsStoryCharacter: boolPtr(false),
		IsMissionOwned:   boolPtr(false),
		InScriptedState:  boolPtr(false),
		InCutscene:       boolPtr(false),
		Blacklisted:      boolPtr(false),
		Visible:          true,
		Metadata:         map[string]any{},
	}
}

// TestGateAllowsSafePed mirrors tests/test_eligibility.py::test_safe_ped_is_allowed.
func TestGateAllowsSafePed(t *testing.T) {
	gate := NewGate(true, nil)
	result := gate.CanActivate(safePed())
	if !result.Eligible {
		t.Fatalf("Eligible = false, want true (reasons: %v)", result.Reasons())
	}
	if result.Reason != "" {
		t.Fatalf("Reason = %q, want empty", result.Reason)
	}
	if reasons := result.Reasons(); len(reasons) != 0 {
		t.Fatalf("Reasons = %v, want empty", reasons)
	}
	if result.Detail["reasons"] == nil {
		t.Fatal("Detail[reasons] must always be present")
	}
}

// TestGateRejectionReasons covers every rejection rule and its exact Python
// reason string.
func TestGateRejectionReasons(t *testing.T) {
	blacklist := NewBlacklist(map[string]any{
		"named_story_characters": map[string]any{
			"models": []any{},
			"names":  []any{"Dutch van der Linde"},
		},
		"mission_only_models": map[string]any{
			"models": []any{"mp_*"},
			"names":  []any{},
		},
	})

	cases := []struct {
		name        string
		ped         func() domain.PedSnapshot
		wantReason  string
		wantReasons []string
	}{
		{
			name:       "empty entity id",
			ped:        func() domain.PedSnapshot { ped := safePed(); ped.EntityID = ""; return ped },
			wantReason: "entity_id is empty",
		},
		{
			name:       "is_ped false",
			ped:        func() domain.PedSnapshot { ped := safePed(); ped.IsPed = boolPtr(false); return ped },
			wantReason: "is_ped is not confirmed true",
		},
		{
			name:       "is_ped unknown",
			ped:        func() domain.PedSnapshot { ped := safePed(); ped.IsPed = nil; return ped },
			wantReason: "is_ped is not confirmed true",
		},
		{
			name:       "is_human false",
			ped:        func() domain.PedSnapshot { ped := safePed(); ped.IsHuman = boolPtr(false); return ped },
			wantReason: "is_human is not confirmed true",
		},
		{
			name:       "is_alive false",
			ped:        func() domain.PedSnapshot { ped := safePed(); ped.IsAlive = boolPtr(false); return ped },
			wantReason: "is_alive is not confirmed true",
		},
		{
			name:       "is_player true",
			ped:        func() domain.PedSnapshot { ped := safePed(); ped.IsPlayer = boolPtr(true); return ped },
			wantReason: "is_player is not confirmed false",
		},
		{
			name:       "is_player unknown",
			ped:        func() domain.PedSnapshot { ped := safePed(); ped.IsPlayer = nil; return ped },
			wantReason: "is_player is not confirmed false",
		},
		{
			name:       "story character",
			ped:        func() domain.PedSnapshot { ped := safePed(); ped.IsStoryCharacter = boolPtr(true); return ped },
			wantReason: "is_story_character is not confirmed false",
		},
		{
			name:       "mission owned",
			ped:        func() domain.PedSnapshot { ped := safePed(); ped.IsMissionOwned = boolPtr(true); return ped },
			wantReason: "is_mission_owned is not confirmed false",
		},
		{
			name:       "mission owned unknown",
			ped:        func() domain.PedSnapshot { ped := safePed(); ped.IsMissionOwned = nil; return ped },
			wantReason: "is_mission_owned is not confirmed false",
		},
		{
			name:       "scripted state",
			ped:        func() domain.PedSnapshot { ped := safePed(); ped.InScriptedState = boolPtr(true); return ped },
			wantReason: "in_scripted_state is not confirmed false",
		},
		{
			name:       "cutscene",
			ped:        func() domain.PedSnapshot { ped := safePed(); ped.InCutscene = boolPtr(true); return ped },
			wantReason: "in_cutscene is not confirmed false",
		},
		{
			name:       "bridge blacklist flag",
			ped:        func() domain.PedSnapshot { ped := safePed(); ped.Blacklisted = boolPtr(true); return ped },
			wantReason: "blacklisted is not confirmed false",
			wantReasons: []string{
				"blacklisted is not confirmed false",
				"matches story/blacklist configuration",
			},
		},
		{
			name:       "blacklisted model pattern",
			ped:        func() domain.PedSnapshot { ped := safePed(); ped.Model = "mp_fake_model"; return ped },
			wantReason: "matches story/blacklist configuration",
		},
		{
			name:       "blacklisted name",
			ped:        func() domain.PedSnapshot { ped := safePed(); ped.Name = "dutch van der linde"; return ped },
			wantReason: "matches story/blacklist configuration",
		},
		{
			name:       "metadata mission entity",
			ped:        metadataPed("mission_entity"),
			wantReason: "metadata.mission_entity is true",
		},
		{
			name:       "metadata is_mission_entity",
			ped:        metadataPed("is_mission_entity"),
			wantReason: "metadata.is_mission_entity is true",
		},
		{
			name:       "metadata scripted",
			ped:        metadataPed("scripted"),
			wantReason: "metadata.scripted is true",
		},
		{
			name:       "metadata is_scripted",
			ped:        metadataPed("is_scripted"),
			wantReason: "metadata.is_scripted is true",
		},
		{
			name:       "metadata cutscene",
			ped:        metadataPed("cutscene"),
			wantReason: "metadata.cutscene is true",
		},
		{
			name:       "metadata is_cutscene",
			ped:        metadataPed("is_cutscene"),
			wantReason: "metadata.is_cutscene is true",
		},
		{
			name:       "metadata critical",
			ped:        metadataPed("critical"),
			wantReason: "metadata.critical is true",
		},
		{
			name:       "metadata is_critical",
			ped:        metadataPed("is_critical"),
			wantReason: "metadata.is_critical is true",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			gate := NewGate(true, blacklist)
			result := gate.CanActivate(testCase.ped())
			if result.Eligible {
				t.Fatalf("Eligible = true, want false (reason %q)", testCase.wantReason)
			}
			if result.Reason != testCase.wantReason {
				t.Fatalf("Reason = %q, want %q (all: %v)", result.Reason, testCase.wantReason, result.Reasons())
			}
			if !containsReason(result.Reasons(), testCase.wantReason) {
				t.Fatalf("Reasons = %v, want to contain %q", result.Reasons(), testCase.wantReason)
			}
			if testCase.wantReasons != nil && !reflect.DeepEqual(result.Reasons(), testCase.wantReasons) {
				t.Fatalf("Reasons = %v, want %v", result.Reasons(), testCase.wantReasons)
			}
		})
	}
}

func metadataPed(flag string) func() domain.PedSnapshot {
	return func() domain.PedSnapshot {
		ped := safePed()
		ped.Metadata = map[string]any{flag: true}
		return ped
	}
}

// TestGateUnknownFieldReason covers the aggregate uncertain-fields reason, its
// position at the end of the require_* reasons and its deduplication.
func TestGateUnknownFieldReason(t *testing.T) {
	cases := []struct {
		name        string
		ped         func() domain.PedSnapshot
		wantReasons []string
	}{
		{
			name: "single unknown field",
			ped:  func() domain.PedSnapshot { ped := safePed(); ped.IsMissionOwned = nil; return ped },
			wantReasons: []string{
				"is_mission_owned is not confirmed false",
				"one or more eligibility fields are unknown",
			},
		},
		{
			name: "many unknown fields keep one aggregate reason",
			ped: func() domain.PedSnapshot {
				ped := safePed()
				ped.IsPed = nil
				ped.IsHuman = nil
				ped.IsAlive = nil
				ped.IsPlayer = nil
				ped.IsStoryCharacter = nil
				ped.IsMissionOwned = nil
				ped.InScriptedState = nil
				ped.InCutscene = nil
				ped.Blacklisted = nil
				return ped
			},
			wantReasons: []string{
				"is_ped is not confirmed true",
				"is_human is not confirmed true",
				"is_alive is not confirmed true",
				"is_player is not confirmed false",
				"is_story_character is not confirmed false",
				"is_mission_owned is not confirmed false",
				"in_scripted_state is not confirmed false",
				"in_cutscene is not confirmed false",
				"blacklisted is not confirmed false",
				"one or more eligibility fields are unknown",
			},
		},
		{
			name: "metadata reasons come before the blacklist reason",
			ped: func() domain.PedSnapshot {
				ped := safePed()
				ped.Metadata = map[string]any{"scripted": true}
				ped.Model = "mp_fake"
				return ped
			},
			wantReasons: []string{
				"metadata.scripted is true",
				"matches story/blacklist configuration",
			},
		},
	}
	blacklist := NewBlacklist(map[string]any{
		"mission_only_models": map[string]any{"models": []any{"mp_*"}},
	})
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := NewGate(true, blacklist).CanActivate(testCase.ped())
			if got := result.Reasons(); !reflect.DeepEqual(got, testCase.wantReasons) {
				t.Fatalf("Reasons = %v, want %v", got, testCase.wantReasons)
			}
			if result.Reason != testCase.wantReasons[0] {
				t.Fatalf("Reason = %q, want %q", result.Reason, testCase.wantReasons[0])
			}
		})
	}
}

// TestGateBlockIfUncertain documents that blockIfUncertain only controls the
// aggregate reason: a missing field still fails the individual requirement, so
// the decision is identical either way.
func TestGateBlockIfUncertain(t *testing.T) {
	uncertain := safePed()
	uncertain.IsMissionOwned = nil

	strict := NewGate(true, nil).CanActivate(uncertain)
	lenient := NewGate(false, nil).CanActivate(uncertain)

	if strict.Eligible || lenient.Eligible {
		t.Fatalf("Eligible = (%v, %v), want both false", strict.Eligible, lenient.Eligible)
	}
	if !containsReason(strict.Reasons(), "one or more eligibility fields are unknown") {
		t.Fatalf("strict reasons = %v, want the aggregate unknown reason", strict.Reasons())
	}
	if containsReason(lenient.Reasons(), "one or more eligibility fields are unknown") {
		t.Fatalf("lenient reasons = %v, want no aggregate unknown reason", lenient.Reasons())
	}

	// A fully specified ped is allowed with either setting.
	lenientGate := NewGate(false, nil)
	if result := lenientGate.CanActivate(safePed()); !result.Eligible {
		t.Fatalf("lenient gate rejected a safe ped: %v", result.Reasons())
	}
}

// TestGateMetadataStrictTrue mirrors Python's "is True" check: a truthy
// non-bool value must not block.
func TestGateMetadataStrictTrue(t *testing.T) {
	cases := []struct {
		name     string
		value    any
		wantGood bool
	}{
		{name: "bool true blocks", value: true, wantGood: false},
		{name: "bool false allows", value: false, wantGood: true},
		{name: "string true allows", value: "true", wantGood: true},
		{name: "number one allows", value: 1, wantGood: true},
		{name: "nil allows", value: nil, wantGood: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ped := safePed()
			ped.Metadata = map[string]any{"scripted": testCase.value}
			result := NewGate(true, nil).CanActivate(ped)
			if result.Eligible != testCase.wantGood {
				t.Fatalf("Eligible = %v, want %v (reasons: %v)", result.Eligible, testCase.wantGood, result.Reasons())
			}
		})
	}
}

// TestGateBlacklistSources mirrors tests/test_eligibility.py's blacklist tests
// plus the bridge-supplied story-character flag.
func TestGateBlacklistSources(t *testing.T) {
	cases := []struct {
		name      string
		blacklist *Blacklist
		ped       func() domain.PedSnapshot
		want      bool
	}{
		{
			name:      "no blacklist allows an ordinary ped",
			blacklist: nil,
			ped:       safePed,
			want:      true,
		},
		{
			name: "story name blocks",
			blacklist: NewBlacklist(map[string]any{
				"named_story_characters": map[string]any{"models": []any{}, "names": []any{"Dutch van der Linde"}},
			}),
			ped: func() domain.PedSnapshot {
				ped := safePed()
				ped.Name = "Dutch van der Linde"
				return ped
			},
			want: false,
		},
		{
			name: "wildcard model blocks",
			blacklist: NewBlacklist(map[string]any{
				"mission_only_models": map[string]any{"models": []any{"mp_*"}, "names": []any{}},
			}),
			ped: func() domain.PedSnapshot {
				ped := safePed()
				ped.Model = "mp_fake_model"
				return ped
			},
			want: false,
		},
		{
			name: "story character flag blocks",
			ped: func() domain.PedSnapshot {
				ped := safePed()
				ped.IsStoryCharacter = boolPtr(true)
				return ped
			},
			want: false,
		},
		{
			name: "safe ped is untouched by an unrelated blacklist",
			blacklist: NewBlacklist(map[string]any{
				"mission_only_models": map[string]any{"models": []any{"mp_*", "cs_*"}, "names": []any{"Dutch van der Linde"}},
			}),
			ped:  safePed,
			want: true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := NewGate(true, testCase.blacklist).CanActivate(testCase.ped())
			if result.Eligible != testCase.want {
				t.Fatalf("Eligible = %v, want %v (reasons: %v)", result.Eligible, testCase.want, result.Reasons())
			}
		})
	}
}

// TestBlacklistMatching covers the blacklist accessors and case handling.
func TestBlacklistMatching(t *testing.T) {
	blacklist := NewBlacklist(map[string]any{
		"protagonists": map[string]any{
			"models": []any{"player_zero", "player_three"},
			"names":  []any{"Arthur Morgan", "John Marston"},
		},
		"mission_only_models": map[string]any{"models": []any{"mp_*", "cs_*"}},
		"not_a_category":      "ignored",
	})

	if got := blacklist.Models(); !reflect.DeepEqual(got, []string{"mp_*", "cs_*", "player_zero", "player_three"}) {
		t.Fatalf("Models = %v", got)
	}
	if got := blacklist.Names(); !reflect.DeepEqual(got, []string{"arthur morgan", "john marston"}) {
		t.Fatalf("Names = %v", got)
	}

	modelCases := []struct {
		model string
		want  bool
	}{
		{"player_zero", true},
		{"PLAYER_ZERO", true},
		{"mp_fake_model", true},
		{"cs_abc", true},
		{"a_m_m_farmer_01", false},
		{"", false},
	}
	for _, testCase := range modelCases {
		if got := blacklist.MatchesModel(testCase.model); got != testCase.want {
			t.Errorf("MatchesModel(%q) = %v, want %v", testCase.model, got, testCase.want)
		}
	}

	nameCases := []struct {
		name string
		want bool
	}{
		{"Arthur Morgan", true},
		{"arthur morgan", true},
		{"ARTHUR MORGAN", true},
		{"Elias Carter", false},
	}
	for _, testCase := range nameCases {
		if got := blacklist.MatchesName(testCase.name); got != testCase.want {
			t.Errorf("MatchesName(%q) = %v, want %v", testCase.name, got, testCase.want)
		}
	}

	var nilBlacklist *Blacklist
	if nilBlacklist.MatchesModel("mp_x") || nilBlacklist.MatchesName("Arthur Morgan") {
		t.Fatal("a nil blacklist must not match anything")
	}
	if nilBlacklist.IsBlacklisted(safePed()) {
		t.Fatal("a nil blacklist must not flag a safe ped")
	}
	if len(nilBlacklist.Models()) != 0 || len(nilBlacklist.Names()) != 0 {
		t.Fatal("a nil blacklist must expose empty patterns")
	}
}

// TestBlacklistFromFile covers the real config file and the fail-open file
// handling required by the Go API.
func TestBlacklistFromFile(t *testing.T) {
	t.Run("repo config", func(t *testing.T) {
		path := filepath.Join("..", "..", "..", "config", "story_blacklist.json")
		if _, err := os.Stat(path); err != nil {
			t.Skipf("config file not available: %v", err)
		}
		blacklist, err := BlacklistFromFile(path)
		if err != nil {
			t.Fatalf("BlacklistFromFile error: %v", err)
		}
		if !blacklist.MatchesModel("mp_fake_model") {
			t.Error("mission-only models must be blacklisted")
		}
		if !blacklist.MatchesModel("cs_character") {
			t.Error("cutscene models must be blacklisted")
		}
		if !blacklist.MatchesName("Dutch van der Linde") {
			t.Error("gang members must be blacklisted by name")
		}
		if !blacklist.MatchesName("Rains Fall") {
			t.Error("named story characters must be blacklisted by name")
		}
		if blacklist.MatchesModel("a_m_m_farmer_01") || blacklist.MatchesName("Elias Carter") {
			t.Error("an ordinary ped must not be blacklisted")
		}
		if len(blacklist.Models()) == 0 || len(blacklist.Names()) == 0 {
			t.Fatal("the repo config must populate models and names")
		}
	})

	t.Run("valid temp file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "story_blacklist.json")
		payload := `{"mission_only_models": {"models": ["MP_*"], "names": ["Someone"]}}`
		if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
			t.Fatalf("write temp blacklist: %v", err)
		}
		blacklist, err := BlacklistFromFile(path)
		if err != nil {
			t.Fatalf("BlacklistFromFile error: %v", err)
		}
		if !blacklist.MatchesModel("mp_thing") || !blacklist.MatchesName("someone") {
			t.Fatalf("patterns = %v / %v, want case-normalised matches", blacklist.Models(), blacklist.Names())
		}
	})

	t.Run("missing file is empty and not an error", func(t *testing.T) {
		blacklist, err := BlacklistFromFile(filepath.Join(t.TempDir(), "does_not_exist.json"))
		if err != nil {
			t.Fatalf("BlacklistFromFile error: %v, want nil", err)
		}
		if blacklist == nil {
			t.Fatal("BlacklistFromFile returned nil blacklist")
		}
		if len(blacklist.Models()) != 0 || len(blacklist.Names()) != 0 {
			t.Fatal("missing file must yield an empty blacklist")
		}
	})

	t.Run("corrupt file is empty and not an error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "corrupt.json")
		if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
			t.Fatalf("write temp blacklist: %v", err)
		}
		blacklist, err := BlacklistFromFile(path)
		if err != nil {
			t.Fatalf("BlacklistFromFile error: %v, want nil", err)
		}
		if len(blacklist.Models()) != 0 || len(blacklist.Names()) != 0 {
			t.Fatal("corrupt file must yield an empty blacklist")
		}
	})

	t.Run("non-object file is empty and not an error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "list.json")
		if err := os.WriteFile(path, []byte(`["mp_*"]`), 0o600); err != nil {
			t.Fatalf("write temp blacklist: %v", err)
		}
		blacklist, err := BlacklistFromFile(path)
		if err != nil {
			t.Fatalf("BlacklistFromFile error: %v, want nil", err)
		}
		if len(blacklist.Models()) != 0 {
			t.Fatal("non-object file must yield an empty blacklist")
		}
	})
}

// TestGlobMatch pins Python fnmatch.fnmatchcase semantics.  The expectations
// were generated with CPython's fnmatch.
func TestGlobMatch(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		want    bool
	}{
		{name: "mp_fake_model", pattern: "mp_*", want: true},
		{name: "mp_", pattern: "mp_*", want: true},
		{name: "cs_abc", pattern: "cs_*", want: true},
		{name: "a_m_m_farmer_01", pattern: "mp_*", want: false},
		{name: "a_c_bear_01", pattern: "a_c_bear_01", want: true},
		{name: "g_m_m_uniduster_01", pattern: "g_m_m_uniduster_01", want: true},
		{name: "dutch van der linde", pattern: "dutch van der linde", want: true},
		{name: "r", pattern: "r*", want: true},
		{name: "r*", pattern: "r*", want: true},
		{name: "rains fall", pattern: "r*", want: true},
		{name: "arthur morgan", pattern: "r*", want: false},
		{name: "michael", pattern: "m?chael", want: true},
		{name: "mchael", pattern: "m?chael", want: false},
		{name: "", pattern: "*", want: true},
		{name: "", pattern: "", want: true},
		{name: "abc", pattern: "a?c", want: true},
		{name: "a_c_wolf_02", pattern: "a_c_wolf", want: false},
		{name: "a_c_wolf", pattern: "a_c_wolf*", want: true},
		{name: "abc", pattern: "[a-c]bc", want: true},
		{name: "dbc", pattern: "[a-c]bc", want: false},
		{name: "dbc", pattern: "[!a-c]bc", want: true},
		{name: "abc", pattern: "[!a-c]bc", want: false},
		{name: "]bc", pattern: "[]]bc", want: true},
		{name: "abc", pattern: "[abc]bc", want: true},
		{name: "a-bc", pattern: "[a-]bc", want: false},
		{name: "-bc", pattern: "[a-]bc", want: true},
		{name: "abc", pattern: "[a", want: false},
		{name: "[", pattern: "[a", want: false},
		{name: "[a", pattern: "[a", want: true},
		{name: "player_zero", pattern: "player_zero", want: true},
		{name: "player_zero", pattern: "player_*", want: true},
		// ``*`` also spans separators in fnmatch, unlike Go's path.Match.
		{name: "a/b", pattern: "a*b", want: true},
		// Matching is case-sensitive; callers lower-case first.
		{name: "MP_X", pattern: "mp_*", want: false},
		// Unicode is matched by rune, not byte.
		{name: "é", pattern: "?", want: true},
		{name: "ß", pattern: "?", want: true},
	}
	for _, testCase := range cases {
		if got := globMatch(testCase.name, testCase.pattern); got != testCase.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", testCase.name, testCase.pattern, got, testCase.want)
		}
	}
}

// TestGateNilAndZeroValue ensures neither a nil gate nor a zero Gate panics.
func TestGateNilAndZeroValue(t *testing.T) {
	var nilGate *Gate
	if result := nilGate.CanActivate(safePed()); !result.Eligible {
		t.Fatalf("nil gate rejected a fully specified ped: %v", result.Reasons())
	}
	var zeroGate Gate
	if result := zeroGate.CanActivate(safePed()); !result.Eligible {
		t.Fatalf("zero gate rejected a fully specified ped: %v", result.Reasons())
	}
	unsafe := safePed()
	unsafe.InCutscene = boolPtr(true)
	if result := nilGate.CanActivate(unsafe); result.Eligible {
		t.Fatal("nil gate accepted a cutscene ped")
	}
}

// TestResultReasonsIsACopy keeps the reported reasons immutable by callers.
func TestResultReasonsIsACopy(t *testing.T) {
	ped := safePed()
	ped.InCutscene = boolPtr(true)
	result := NewGate(true, nil).CanActivate(ped)
	reasons := result.Reasons()
	if len(reasons) != 1 || reasons[0] != "in_cutscene is not confirmed false" {
		t.Fatalf("Reasons = %v", reasons)
	}
	reasons[0] = "mutated"
	if got := result.Reasons()[0]; got != "in_cutscene is not confirmed false" {
		t.Fatalf("Reasons() = %q, want the original reason", got)
	}
}

// TestNewBlacklistDeterministic checks the sorted flattening used to keep the
// Go port deterministic.
func TestNewBlacklistDeterministic(t *testing.T) {
	categories := map[string]any{
		"b": map[string]any{"models": []any{"b_*"}},
		"a": map[string]any{"models": []any{"a_*"}},
		"c": map[string]any{"names": []any{"C Name"}},
	}
	first := NewBlacklist(categories).Models()
	for attempt := 0; attempt < 20; attempt++ {
		if got := NewBlacklist(categories).Models(); !reflect.DeepEqual(got, first) {
			t.Fatalf("Models = %v, want stable %v", got, first)
		}
	}
	if !reflect.DeepEqual(first, []string{"a_*", "b_*"}) {
		t.Fatalf("Models = %v, want [a_* b_*]", first)
	}
}
