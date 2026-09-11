package state

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

// eligibilityUnknownReason is the aggregate reason the gate adds when
// block_if_uncertain is on and any safety field is missing.
const eligibilityUnknownReason = "one or more eligibility fields are unknown"

// eligibilityBlacklistReason is the reason reported when the story blacklist
// (or a bridge-supplied blacklist flag) matches.
const eligibilityBlacklistReason = "matches story/blacklist configuration"

// Result is the outcome of one eligibility check.
//
// It mirrors the Python "EligibilityResult": Eligible is "allowed", Reason
// is the primary (first) rejection reason and is empty when the ped is
// eligible, and Detail always carries the full ordered reason list under the
// ""reasons"" key.
type Result struct {
	Eligible bool           `json:"eligible"`
	Reason   string         `json:"reason"`
	Detail   map[string]any `json:"detail"`
}

// Reasons returns the full ordered list of rejection reasons, empty when the
// ped is eligible.
func (r Result) Reasons() []string {
	raw, ok := r.Detail["reasons"]
	if !ok {
		return []string{}
	}
	switch typed := raw.(type) {
	case []string:
		out := make([]string, len(typed))
		copy(out, typed)
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, domain.StringFrom(item))
		}
		return out
	default:
		return []string{}
	}
}

// Blacklist is the model/name story blacklist loaded from
// "config/story_blacklist.json".
//
// Patterns are glob patterns matched with Python "fnmatch.fnmatchcase"
// semantics against the lower-cased model/name, so "mp_*" blocks every
// mission-only model.
type Blacklist struct {
	categories map[string]any
	models     []string
	names      []string
}

// NewBlacklist flattens a blacklist configuration, which is a mapping of
// category name to an object with optional "models" and "names" arrays.
// Category keys are visited in sorted order so the flattened lists are
// deterministic; a nil map yields an empty blacklist.
func NewBlacklist(categories map[string]any) *Blacklist {
	blacklist := &Blacklist{categories: categories}
	keys := make([]string, 0, len(categories))
	for key := range categories {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		category, ok := categories[key].(map[string]any)
		if !ok {
			continue
		}
		for _, model := range domain.StringsFrom(category["models"]) {
			blacklist.models = append(blacklist.models, strings.ToLower(model))
		}
		for _, name := range domain.StringsFrom(category["names"]) {
			blacklist.names = append(blacklist.names, strings.ToLower(name))
		}
	}
	return blacklist
}

// BlacklistFromFile loads "config/story_blacklist.json".
//
// A missing, unreadable, corrupt or non-object file yields an empty blacklist
// and a nil error: an unreadable blacklist must never stop the runtime, and
// the eligibility gate is conservative regardless.
func BlacklistFromFile(path string) (*Blacklist, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return NewBlacklist(nil), nil
	}
	var categories map[string]any
	if err := json.Unmarshal(data, &categories); err != nil {
		return NewBlacklist(nil), nil
	}
	return NewBlacklist(categories), nil
}

// Models returns the lower-cased model patterns in the blacklist.
func (b *Blacklist) Models() []string {
	if b == nil {
		return []string{}
	}
	out := make([]string, len(b.models))
	copy(out, b.models)
	return out
}

// Names returns the lower-cased name patterns in the blacklist.
func (b *Blacklist) Names() []string {
	if b == nil {
		return []string{}
	}
	out := make([]string, len(b.names))
	copy(out, b.names)
	return out
}

// MatchesModel reports whether a ped model matches a blacklisted pattern.
func (b *Blacklist) MatchesModel(model string) bool {
	if b == nil {
		return false
	}
	modelLower := strings.ToLower(model)
	for _, pattern := range b.models {
		if globMatch(modelLower, pattern) {
			return true
		}
	}
	return false
}

// MatchesName reports whether a ped name matches a blacklisted pattern.
func (b *Blacklist) MatchesName(name string) bool {
	if b == nil {
		return false
	}
	nameLower := strings.ToLower(name)
	for _, pattern := range b.names {
		if globMatch(nameLower, pattern) {
			return true
		}
	}
	return false
}

// IsBlacklisted reports whether the bridge flagged the ped or its model/name
// appears in the story blacklist.  Only a strictly true bridge flag blocks;
// an absent flag is handled by the eligibility gate.
func (b *Blacklist) IsBlacklisted(ped domain.PedSnapshot) bool {
	if ped.Blacklisted != nil && *ped.Blacklisted {
		return true
	}
	if ped.IsStoryCharacter != nil && *ped.IsStoryCharacter {
		return true
	}
	return b.MatchesModel(ped.Model) || b.MatchesName(ped.Name)
}

// Gate decides whether a ped may be activated as an LLM-backed agent.
//
// The gate fails closed: a missing or ambiguous safety field blocks
// activation.  A nil *Gate behaves like an empty gate with blockIfUncertain
// off, and the zero Gate does the same; use NewGate for the conservative
// defaults.
type Gate struct {
	blacklist        *Blacklist
	blockIfUncertain bool
}

// NewGate builds a gate.  A nil blacklist means "no model/name blacklist";
// blockIfUncertain adds the aggregate "fields are unknown" reason described by
// the Python implementation.
func NewGate(blockIfUncertain bool, blacklist *Blacklist) *Gate {
	if blacklist == nil {
		blacklist = NewBlacklist(nil)
	}
	return &Gate{blacklist: blacklist, blockIfUncertain: blockIfUncertain}
}

// CanActivate applies the conservative eligibility rules and reports every
// reason the ped was rejected.
func (g *Gate) CanActivate(ped domain.PedSnapshot) Result {
	blockIfUncertain := false
	var blacklist *Blacklist
	if g != nil {
		blockIfUncertain = g.blockIfUncertain
		blacklist = g.blacklist
	}

	reasons := make([]string, 0, 4)

	requireTrue := func(value *bool, label string) {
		if value == nil || !*value {
			reasons = append(reasons, label+" is not confirmed true")
		}
	}
	requireFalse := func(value *bool, label string) {
		if value == nil || *value {
			reasons = append(reasons, label+" is not confirmed false")
		}
	}

	if ped.EntityID == "" {
		reasons = append(reasons, "entity_id is empty")
	}
	requireTrue(ped.IsPed, "is_ped")
	requireTrue(ped.IsHuman, "is_human")
	requireTrue(ped.IsAlive, "is_alive")
	requireFalse(ped.IsPlayer, "is_player")
	requireFalse(ped.IsStoryCharacter, "is_story_character")
	requireFalse(ped.IsMissionOwned, "is_mission_owned")
	requireFalse(ped.InScriptedState, "in_scripted_state")
	requireFalse(ped.InCutscene, "in_cutscene")
	requireFalse(ped.Blacklisted, "blacklisted")

	if blockIfUncertain && anySafetyFieldUnknown(ped) && !containsReason(reasons, eligibilityUnknownReason) {
		reasons = append(reasons, eligibilityUnknownReason)
	}

	// Optional extra conservative signals supplied by the bridge.  The flag
	// order matches the Python implementation so reason ordering is stable.
	if len(ped.Metadata) > 0 {
		for _, flag := range []string{
			"mission_entity",
			"is_mission_entity",
			"scripted",
			"is_scripted",
			"cutscene",
			"is_cutscene",
			"critical",
			"is_critical",
		} {
			if value, ok := ped.Metadata[flag].(bool); ok && value {
				reasons = append(reasons, "metadata."+flag+" is true")
			}
		}
	}

	if blacklist.IsBlacklisted(ped) {
		reasons = append(reasons, eligibilityBlacklistReason)
	}

	unique := dedupeReasons(reasons)
	result := Result{
		Eligible: len(unique) == 0,
		Detail:   map[string]any{"reasons": unique},
	}
	if len(unique) > 0 {
		result.Reason = unique[0]
	}
	return result
}

// anySafetyFieldUnknown mirrors the Python "any(value is None ...)" check.
func anySafetyFieldUnknown(ped domain.PedSnapshot) bool {
	return ped.IsPed == nil ||
		ped.IsHuman == nil ||
		ped.IsAlive == nil ||
		ped.IsPlayer == nil ||
		ped.IsStoryCharacter == nil ||
		ped.IsMissionOwned == nil ||
		ped.InScriptedState == nil ||
		ped.InCutscene == nil ||
		ped.Blacklisted == nil
}

func containsReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

// dedupeReasons removes duplicates while preserving first-seen order.
func dedupeReasons(reasons []string) []string {
	unique := make([]string, 0, len(reasons))
	seen := make(map[string]struct{}, len(reasons))
	for _, reason := range reasons {
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		unique = append(unique, reason)
	}
	return unique
}

// globMatch reports whether name matches pattern with Python
// "fnmatch.fnmatchcase" semantics: "*" matches any run of characters
// (including none), "?" matches exactly one character, "[seq]" matches one
// character of the set, "[!seq]" negates the set and ranges such as
// "[a-z]" are supported.  An unterminated "[" is a literal.  Matching is
// case-sensitive; callers lower-case both sides first.
func globMatch(name, pattern string) bool {
	nameRunes := []rune(name)
	patternRunes := []rune(pattern)

	type globState struct{ name, pattern int }
	memo := make(map[globState]bool)
	resolved := make(map[globState]bool)

	var match func(nameIndex, patternIndex int) bool
	match = func(nameIndex, patternIndex int) bool {
		key := globState{nameIndex, patternIndex}
		if resolved[key] {
			return memo[key]
		}
		resolved[key] = true

		result := false
		switch {
		case patternIndex == len(patternRunes):
			result = nameIndex == len(nameRunes)
		case patternRunes[patternIndex] == '*':
			result = match(nameIndex, patternIndex+1) ||
				(nameIndex < len(nameRunes) && match(nameIndex+1, patternIndex))
		case patternRunes[patternIndex] == '?':
			result = nameIndex < len(nameRunes) && match(nameIndex+1, patternIndex+1)
		case patternRunes[patternIndex] == '[':
			end, ok := classEnd(patternRunes, patternIndex)
			if !ok {
				// Unterminated class: Python treats ``[`` as a literal.
				result = nameIndex < len(nameRunes) &&
					nameRunes[nameIndex] == '[' && match(nameIndex+1, patternIndex+1)
			} else {
				result = nameIndex < len(nameRunes) &&
					classContains(patternRunes[patternIndex+1:end], nameRunes[nameIndex]) &&
					match(nameIndex+1, end+1)
			}
		default:
			result = nameIndex < len(nameRunes) &&
				nameRunes[nameIndex] == patternRunes[patternIndex] && match(nameIndex+1, patternIndex+1)
		}

		memo[key] = result
		return result
	}

	return match(0, 0)
}

// classEnd returns the index of the "]" that closes the character class
// starting at pattern[start] (a "["), reporting false when the class is
// unterminated.  A "!" immediately after "[" negates the class and a "]"
// immediately after that is a literal member, exactly like Python.
func classEnd(pattern []rune, start int) (int, bool) {
	index := start + 1
	if index < len(pattern) && pattern[index] == '!' {
		index++
	}
	if index < len(pattern) && pattern[index] == ']' {
		index++
	}
	for ; index < len(pattern); index++ {
		if pattern[index] == ']' {
			return index, true
		}
	}
	return 0, false
}

// classContains reports whether ch is a member of the class body (the runes
// between "["/"[!" and "]").
func classContains(body []rune, ch rune) bool {
	negate := false
	if len(body) > 0 && body[0] == '!' {
		negate = true
		body = body[1:]
	}
	found := false
	for index := 0; index < len(body); {
		if index+2 < len(body) && body[index+1] == '-' {
			if ch >= body[index] && ch <= body[index+2] {
				found = true
			}
			index += 3
			continue
		}
		if body[index] == ch {
			found = true
		}
		index++
	}
	if negate {
		return !found
	}
	return found
}
