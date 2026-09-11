// Package events converts bridge messages into canonical world state and
// timeline events.
//
// It is a faithful port of `runtime/events/normalizer.py`. The normalizer
// deliberately preserves fact-shaped data: it does not assign moral labels
// such as PLAYER_IS_EVIL to observations.
package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

// eventNamePattern mirrors the Python validator `^[A-Z][A-Z0-9_]*$`. Go's
// `$` anchors strictly at the end of the text while Python's also matches
// just before one trailing newline, hence the optional `\n`, which makes the
// two languages accept exactly the same names.
var eventNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*\n?$`)

// Normalizer turns raw bridge payloads into canonical runtime data. It holds no
// state and is safe for concurrent use.
type Normalizer struct{}

// NewNormalizer returns a ready normalizer.
func NewNormalizer() *Normalizer {
	return &Normalizer{}
}

// WorldStateFromMessage normalises a `world_update` payload. A missing
// npc_id is an error, matching Python.
func (n *Normalizer) WorldStateFromMessage(raw map[string]any) (domain.WorldState, error) {
	npcID, err := requireNPCID(raw, "message is missing npc_id")
	if err != nil {
		return domain.WorldState{}, err
	}
	return domain.WorldStateFromBridgePayload(npcID, raw), nil
}

// EventFromMessage builds an event from a `game_event` (or similar) payload.
//
// defaultNPCID is used when the payload carries no npc_id. Event names are
// upper-cased and validated, data must be an object, and a single string
// entity or tag becomes a one-element list.
func (n *Normalizer) EventFromMessage(raw map[string]any, seq int, defaultNPCID string) (domain.Event, error) {
	npcValue := raw["npc_id"]
	if !truthy(npcValue) {
		npcValue = defaultNPCID
	}
	if !truthy(npcValue) {
		return domain.Event{}, errors.New("game event is missing npc_id")
	}
	npcID := domain.StringFrom(npcValue)

	nameValue := raw["event_name"]
	if !truthy(nameValue) {
		nameValue = raw["name"]
	}
	if !truthy(nameValue) {
		return domain.Event{}, errors.New("game event is missing event_name")
	}
	eventName := strings.ToUpper(domain.StringFrom(nameValue))
	if !eventNamePattern.MatchString(eventName) {
		return domain.Event{}, fmt.Errorf("invalid event_name: %q", eventName)
	}

	data := map[string]any{}
	if dataValue := raw["data"]; truthy(dataValue) {
		typed, ok := dataValue.(map[string]any)
		if !ok {
			return domain.Event{}, errors.New("event data must be an object")
		}
		data = typed
	}

	entities, err := stringList(raw["entities"], "entities")
	if err != nil {
		return domain.Event{}, err
	}
	tags, err := stringList(raw["tags"], "tags")
	if err != nil {
		return domain.Event{}, err
	}

	var gameTime *domain.GameTime
	if value := raw["game_time"]; truthy(value) {
		typed, ok := value.(map[string]any)
		if !ok {
			return domain.Event{}, errors.New("game_time must be an object")
		}
		gameTime = domain.GameTimeFromMap(typed)
	}
	var location *domain.Location
	if value := raw["location"]; truthy(value) {
		typed, ok := value.(map[string]any)
		if !ok {
			return domain.Event{}, errors.New("location must be an object")
		}
		location = domain.LocationFromMap(typed)
	}

	importance, err := importanceFrom(raw["importance"])
	if err != nil {
		return domain.Event{}, err
	}

	// Python copies the payload's containers; keep the stored event from
	// aliasing the caller's map.
	dataCopy := make(map[string]any, len(data))
	for key, value := range data {
		dataCopy[key] = value
	}

	var timestamp *float64
	if value, ok := raw["timestamp"]; ok && value != nil {
		parsed := domain.FloatFrom(value)
		timestamp = &parsed
	}

	// Python builds the Event last, so the seq invariant is reported after the
	// payload validations above.
	if seq < 1 {
		return domain.Event{}, errors.New("seq must be >= 1")
	}

	return domain.NewEvent(domain.NewEventOptions{
		Seq:        seq,
		EventName:  eventName,
		NPCID:      npcID,
		Data:       dataCopy,
		Entities:   entities,
		Tags:       tags,
		Summary:    summaryFrom(raw["summary"]),
		Importance: importance,
		GameTime:   gameTime,
		Location:   location,
		Timestamp:  timestamp,
		EventID:    domain.StringFrom(raw["event_id"]),
	}), nil
}

// PlayerSpoke builds the standard fact event for player speech.
//
// The data merges extra over the generated text key, mirroring Python's
// `{"text": text, **data}`. Python's call signature rejects a "text" key in
// extra with a TypeError, so a well-formed Python call can never disagree with
// this precedence.
func (n *Normalizer) PlayerSpoke(npcID, text string, extra map[string]any) map[string]any {
	return map[string]any{
		"event_name": "PLAYER_SPOKE",
		"npc_id":     npcID,
		"entities":   []string{"player"},
		"tags":       []string{"player", "speech"},
		"data":       mergeText(text, extra),
	}
}

// NPCSpoke builds the standard fact event for NPC speech. Its data merges extra
// over the generated text key, like PlayerSpoke.
func (n *Normalizer) NPCSpoke(npcID, text string, extra map[string]any) map[string]any {
	return map[string]any{
		"event_name": "NPC_SPOKE",
		"npc_id":     npcID,
		"entities":   []string{npcID},
		"tags":       []string{"npc", "speech"},
		"data":       mergeText(text, extra),
	}
}

// ActionResult maps a bridge action result onto an ACTION_STARTED,
// ACTION_COMPLETED or ACTION_FAILED event payload. The result keeps the raw
// extra fields so the timeline stores observable facts, and carries summary and
// importance through unchanged.
func (n *Normalizer) ActionResult(raw map[string]any) map[string]any {
	var eventName string
	switch strings.ToLower(domain.StringFrom(raw["status"])) {
	case "completed":
		eventName = "ACTION_COMPLETED"
	case "failed":
		eventName = "ACTION_FAILED"
	default:
		eventName = "ACTION_STARTED"
	}

	data := map[string]any{
		"tool":       raw["tool"],
		"request_id": raw["request_id"],
	}
	for key, value := range raw {
		switch key {
		case "type", "npc_id", "status", "tool", "request_id":
			continue
		}
		data[key] = value
	}

	return map[string]any{
		"event_name": eventName,
		"npc_id":     raw["npc_id"],
		"tags":       []string{"action"},
		"data":       data,
		"summary":    raw["summary"],
		"importance": raw["importance"],
	}
}

// GoalEvent builds a goal-tagged event payload.
func (n *Normalizer) GoalEvent(npcID, eventName string, data map[string]any) map[string]any {
	payload := make(map[string]any, len(data))
	for key, value := range data {
		payload[key] = value
	}
	return map[string]any{
		"event_name": eventName,
		"npc_id":     npcID,
		"tags":       []string{"goal"},
		"data":       payload,
	}
}

// requireNPCID reads and stringifies npc_id, rejecting every value Python would
// consider falsy.
func requireNPCID(raw map[string]any, message string) (string, error) {
	value := raw["npc_id"]
	if !truthy(value) {
		return "", errors.New(message)
	}
	return domain.StringFrom(value), nil
}

// mergeText mirrors Python's `{"text": text, **extra}`: extra wins.
func mergeText(text string, extra map[string]any) map[string]any {
	data := make(map[string]any, len(extra)+1)
	data["text"] = text
	for key, value := range extra {
		data[key] = value
	}
	return data
}

// stringList mirrors `entities = raw.get(field) or []` followed by
// `[entities] if isinstance(entities, str) else list(entities)`. Decoded JSON
// arrays are the normal case; a JSON object is iterable in Python (yielding its
// keys), so its keys are used here too, sorted because Go maps are unordered.
func stringList(value any, field string) ([]string, error) {
	if !truthy(value) {
		return []string{}, nil
	}
	switch typed := value.(type) {
	case string:
		return []string{typed}, nil
	case []string:
		return append([]string{}, typed...), nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, domain.StringFrom(item))
		}
		return out, nil
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return keys, nil
	default:
		// Python raises TypeError for a scalar here; report it as an error
		// rather than silently dropping the fact.
		return nil, fmt.Errorf("%s must be a list or a string", field)
	}
}

// summaryFrom narrows the free-form summary to the canonical *string field.
// Python stores anything; the Go type is a string, so a non-string value is
// stringified rather than lost. An empty summary is kept, because Python only
// drops it when it is None.
func summaryFrom(value any) *string {
	if value == nil {
		return nil
	}
	text := domain.StringFrom(value)
	return &text
}

// importanceFrom mirrors `float(importance)` inside Event.__post_init__:
// a non-numeric string is an error, a bool is 0 or 1, and the result is clamped
// to [0, 1] by the canonical event constructor.
func importanceFrom(value any) (*float64, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case bool:
		score := 0.0
		if typed {
			score = 1
		}
		return &score, nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid importance: %q", typed)
		}
		return &parsed, nil
	case []any, map[string]any:
		// float() rejects both in Python with a TypeError.
		return nil, fmt.Errorf("invalid importance: %v", typed)
	default:
		score := domain.FloatFrom(value)
		return &score, nil
	}
}

// truthy mirrors Python truthiness for the JSON-decoded values the normalizer
// receives, because the original relies on `x or default` in several places.
func truthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case float64:
		return typed != 0
	case float32:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case json.Number:
		parsed, err := typed.Float64()
		return err != nil || parsed != 0
	case []any:
		return len(typed) > 0
	case []string:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}
