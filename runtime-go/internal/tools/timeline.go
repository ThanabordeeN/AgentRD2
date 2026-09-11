package tools

import (
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/timeline"
)

// grabTimelineSchema mirrors “GRAB_TIMELINE_SCHEMA“.
var grabTimelineSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"event_name":  map[string]any{"type": "string"},
		"event_names": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"tag":         map[string]any{"type": "string"},
		"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"entity":      map[string]any{"type": "string"},
		"minimum_importance": map[string]any{
			"type": "number", "minimum": 0.0, "maximum": 1.0,
		},
		"since_seq":  map[string]any{"type": "integer", "minimum": 0},
		"before_seq": map[string]any{"type": "integer", "minimum": 1},
		"limit":      map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "default": 20},
	},
	"additionalProperties": false,
}

// grabTimeline mirrors “runtime/tools/timeline.py::grab_timeline“. The Python
// handler accepts “**filters“ and forwards them to the store, whose keyword
// defaults (notably “limit=20“) fill the gaps; grabFilterFromArgs reproduces
// that mapping onto the typed Go filter.
func grabTimeline(ctx *Context, args map[string]any) domain.ToolResult {
	filter, reason := grabFilterFromArgs(args)
	if reason != "" {
		return failedCall("grab_timeline", ctx, args, reason)
	}
	events, err := ctx.GrabTimeline(filter)
	if err != nil {
		return failedCall("grab_timeline", ctx, args, err.Error())
	}
	return completedData("grab_timeline", ctx, args, map[string]any{"events": eventMaps(events)})
}

// grabFilterFromArgs maps the tool arguments onto “timeline.GrabFilter“,
// reproducing the Python store's keyword defaults: “limit“ defaults to 20,
// every other filter is absent unless supplied, and an unparsable value is
// reported as a failure reason (Python raises inside the store comparison and
// the runtime turns that into a failed result).
func grabFilterFromArgs(args map[string]any) (timeline.GrabFilter, string) {
	filter := timeline.GrabFilter{Limit: grabTimelineDefaultLimit}

	if value, ok := stringArg(args, FilterEventName); ok {
		filter.EventName = value
	}
	if value, ok := raw(args, FilterEventNames); ok {
		filter.EventNames = domain.StringsFrom(value)
	}
	if value, ok := stringArg(args, FilterTag); ok {
		filter.Tag = value
	}
	if value, ok := raw(args, FilterTags); ok {
		filter.Tags = domain.StringsFrom(value)
	}
	if value, ok := stringArg(args, FilterEntity); ok {
		filter.Entity = value
	}
	if provided(args, FilterMinimumImportance) {
		value, ok := numberArg(args, FilterMinimumImportance)
		if !ok {
			return timeline.GrabFilter{}, "invalid argument: " + FilterMinimumImportance
		}
		filter.MinimumImportance = &value
	}
	if provided(args, FilterSinceSeq) {
		value, ok := intArg(args, FilterSinceSeq)
		if !ok {
			return timeline.GrabFilter{}, "invalid argument: " + FilterSinceSeq
		}
		filter.SinceSeq = &value
	}
	if provided(args, FilterBeforeSeq) {
		value, ok := intArg(args, FilterBeforeSeq)
		if !ok {
			return timeline.GrabFilter{}, "invalid argument: " + FilterBeforeSeq
		}
		filter.BeforeSeq = &value
	}
	if provided(args, FilterLimit) {
		value, ok := intArg(args, FilterLimit)
		if !ok {
			return timeline.GrabFilter{}, "invalid argument: " + FilterLimit
		}
		filter.Limit = value
	}
	return filter, ""
}

// RegisterTimelineTools registers the perception tool “grab_timeline“.
func RegisterTimelineTools(registry *Registry) {
	_ = registry.Register(Spec{
		Name: "grab_timeline",
		Description: "Retrieve older named events from this NPC's JSONL timeline. " +
			"Use deterministic filters; no semantic search exists in the MVP.",
		Parameters: grabTimelineSchema,
		Category:   "perception",
		Handler:    grabTimeline,
	})
}
