package domain

import (
	"fmt"
)

// GameTime is the in-game clock attached to an event when the bridge supplies
// one.
type GameTime struct {
	Day    int `json:"day"`
	Hour   int `json:"hour"`
	Minute int `json:"minute"`
}

// ToMap mirrors “GameTime.to_dict“.
func (g GameTime) ToMap() map[string]any {
	return map[string]any{"day": g.Day, "hour": g.Hour, "minute": g.Minute}
}

// GameTimeFromMap mirrors “GameTime.from_dict“; a nil/empty map yields nil.
func GameTimeFromMap(value map[string]any) *GameTime {
	if len(value) == 0 {
		return nil
	}
	return &GameTime{
		Day:    IntFrom(value["day"]),
		Hour:   IntFrom(value["hour"]),
		Minute: IntFrom(value["minute"]),
	}
}

// Location is an optional world position/region stamped onto an event.
type Location struct {
	Region   *string   `json:"region,omitempty"`
	Position []float64 `json:"position,omitempty"`
}

// ToMap mirrors “Location.to_dict“ (nil fields are dropped).
func (l Location) ToMap() map[string]any {
	out := map[string]any{}
	if l.Region != nil {
		out["region"] = *l.Region
	}
	if l.Position != nil {
		out["position"] = l.Position
	}
	return out
}

// LocationFromMap mirrors “Location.from_dict“.
func LocationFromMap(value map[string]any) *Location {
	if len(value) == 0 {
		return nil
	}
	loc := &Location{}
	if region, ok := value["region"].(string); ok {
		loc.Region = &region
	}
	if raw, ok := value["position"].([]any); ok {
		position := make([]float64, 0, len(raw))
		for _, item := range raw {
			position = append(position, FloatFrom(item))
		}
		loc.Position = position
	}
	return loc
}

// Event is one immutable entry in an NPC's JSONL timeline.
//
// “Data“ is deliberately free-form: the bridge and normalizer store
// observable facts, not interpretations.
type Event struct {
	Seq        int            `json:"seq"`
	EventName  string         `json:"event_name"`
	NPCID      string         `json:"npc_id"`
	EventID    string         `json:"event_id"`
	Timestamp  float64        `json:"timestamp"`
	GameTime   *GameTime      `json:"game_time,omitempty"`
	Location   *Location      `json:"location,omitempty"`
	Entities   []string       `json:"entities"`
	Data       map[string]any `json:"data"`
	Summary    *string        `json:"summary,omitempty"`
	Importance *float64       `json:"importance,omitempty"`
	Tags       []string       `json:"tags"`
}

// Validate applies the same invariants as “Event.__post_init__“.
func (e *Event) Validate() error {
	if e.EventName == "" {
		return fmt.Errorf("event_name is required")
	}
	if e.NPCID == "" {
		return fmt.Errorf("npc_id is required")
	}
	if e.Seq < 1 {
		return fmt.Errorf("seq must be >= 1")
	}
	e.Importance = ClampImportance(e.Importance)
	return nil
}

// ToMap mirrors “Event.to_dict“: optional fields are omitted, never null,
// and “data“ is recursively stripped of nil values.
func (e Event) ToMap() map[string]any {
	if e.Entities == nil {
		e.Entities = []string{}
	}
	if e.Tags == nil {
		e.Tags = []string{}
	}
	payload := map[string]any{
		"seq":        e.Seq,
		"event_id":   e.EventID,
		"event_name": e.EventName,
		"timestamp":  e.Timestamp,
		"npc_id":     e.NPCID,
		"entities":   e.Entities,
		"data":       CleanMap(e.Data),
		"tags":       e.Tags,
	}
	if e.GameTime != nil {
		payload["game_time"] = e.GameTime.ToMap()
	}
	if e.Location != nil {
		payload["location"] = e.Location.ToMap()
	}
	if e.Summary != nil {
		payload["summary"] = *e.Summary
	}
	if e.Importance != nil {
		payload["importance"] = *e.Importance
	}
	return payload
}

// EventFromMap mirrors “Event.from_dict“.
func EventFromMap(value map[string]any) (Event, error) {
	event := Event{
		Seq:       IntFrom(value["seq"]),
		EventName: StringFrom(value["event_name"]),
		NPCID:     StringFrom(value["npc_id"]),
		EventID:   StringFrom(value["event_id"]),
		Timestamp: FloatFrom(value["timestamp"]),
	}
	if event.EventID == "" {
		event.EventID = NewEventID()
	}
	if event.Timestamp == 0 {
		event.Timestamp = Timestamp()
	}
	if raw, ok := value["game_time"].(map[string]any); ok {
		event.GameTime = GameTimeFromMap(raw)
	}
	if raw, ok := value["location"].(map[string]any); ok {
		event.Location = LocationFromMap(raw)
	}
	event.Entities = StringsFrom(value["entities"])
	event.Tags = StringsFrom(value["tags"])
	if raw, ok := value["data"].(map[string]any); ok {
		event.Data = raw
	}
	if raw, ok := value["summary"].(string); ok {
		event.Summary = &raw
	}
	if raw, ok := value["importance"]; ok && raw != nil {
		score := FloatFrom(raw)
		event.Importance = &score
	}
	if err := event.Validate(); err != nil {
		return Event{}, err
	}
	return event, nil
}

// NewEvent builds an event with generated defaults, mirroring “new_event“.
type NewEventOptions struct {
	Seq        int
	EventName  string
	NPCID      string
	Data       map[string]any
	Entities   []string
	Tags       []string
	Summary    *string
	Importance *float64
	GameTime   *GameTime
	Location   *Location
	Timestamp  *float64
	EventID    string
}

// NewEvent constructs a validated event.
func NewEvent(opts NewEventOptions) Event {
	event := Event{
		Seq:        opts.Seq,
		EventName:  opts.EventName,
		NPCID:      opts.NPCID,
		EventID:    opts.EventID,
		Timestamp:  Timestamp(),
		GameTime:   opts.GameTime,
		Location:   opts.Location,
		Entities:   opts.Entities,
		Data:       opts.Data,
		Summary:    opts.Summary,
		Importance: opts.Importance,
		Tags:       opts.Tags,
	}
	if event.EventID == "" {
		event.EventID = NewEventID()
	}
	if opts.Timestamp != nil {
		event.Timestamp = *opts.Timestamp
	}
	if event.Entities == nil {
		event.Entities = []string{}
	}
	if event.Tags == nil {
		event.Tags = []string{}
	}
	if event.Data == nil {
		event.Data = map[string]any{}
	}
	_ = event.Validate()
	return event
}
