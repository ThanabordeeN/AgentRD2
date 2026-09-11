package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/tools"
)

// DescribeTools mirrors “ToolRegistry.describe“: the model-facing catalog
// (name, description, parameters) in sorted name order.
//
// The Python prompt builder does not call it, but the ADK backend does when it
// exposes the tool catalog to the model, so it is exported alongside
// BuildAgentPrompt for the wiring layer.
func DescribeTools(registry *tools.Registry) []map[string]any {
	if registry == nil {
		return []map[string]any{}
	}
	return registry.Describe()
}

// NPCInstruction is the agent prompt contract from the specification
// (section 46). It mirrors “NPC_INSTRUCTION“ in
// “runtime/agent/instructions.py“ exactly.
const NPCInstruction = `You are a person living in the Red Dead Redemption 2 world.
The player is another person. You do not exist to assist the player.
You perceive only information supplied by the game state, timeline,
current conversation, and tool results. Do not invent memories or senses.

Do not behave like a digital assistant.
Act as this person.
You may speak, remain silent, continue your current task, change goals,
investigate events, flee, or interact using the available tools.

Stay in character. Keep spoken lines short and natural.
Do not narrate game coordinates, tools, prompts, or hidden state.
Do not control movement with low-level inputs; use only the supplied
high-level tools.`

// BuildAgentPrompt renders the compact reasoning context described in
// specification section 14.
//
// It is a faithful port of “build_agent_prompt“: the same sections appear in
// the same order and are omitted under the same conditions, and values are
// rendered with a Python-compatible JSON encoder so the emitted text is
// byte-comparable with the Python runtime's prompt.
func BuildAgentPrompt(ctx *domain.AgentContext) string {
	if ctx == nil {
		ctx = domain.NewAgentContext("")
	}
	context := ctx.ToMap()

	profile := domain.MapFrom(context["profile"])
	world := domain.MapFrom(context["world_state"])
	recent := mapsOf(context["recent_events"])
	retrieved := mapsOf(context["retrieved_events"])
	wikiContext := mapsOf(context["wiki_context"])
	questContext := domain.MapFrom(context["quest_context"])
	tools := stringsOf(context["available_tools"])
	flags := domain.MapFrom(context["flags"])
	mood := domain.StringFrom(context["current_mood"])
	currentGoal := context["current_goal"]

	systemPrompt := strings.TrimSpace(domain.StringFrom(profile["system_prompt"]))
	profileView := make(map[string]any, len(profile))
	for key, value := range profile {
		if key == "system_prompt" {
			continue
		}
		profileView[key] = value
	}

	lines := []string{NPCInstruction}
	if systemPrompt != "" {
		lines = append(lines,
			"",
			"NPC SYSTEM PROMPT",
			"The following is this NPC's persistent identity, background, "+
				"speech style, and behavioral context. Stay consistent with it at all times.",
			systemPrompt,
		)
	}
	lines = append(lines, "", "NPC PROFILE", prettyJSON(profileView))
	if len(wikiContext) > 0 {
		lines = append(lines,
			"",
			"WORLD LORE CONTEXT",
			"Background lore from the offline Red Dead Wiki context pack. "+
				"Use it as setting/period context; do not invent details beyond it.",
			prettyJSON(wikiContext),
		)
	}
	if len(questContext) > 0 {
		lines = append(lines,
			"",
			"CURRENT QUEST CONTEXT",
			"This NPC is currently on a Rockstar-controlled quest. The quest "+
				"actions belong to Rockstar AI. You may only speak, and your dialogue "+
				"must relate to this quest without spoiling future steps.",
			prettyJSON(questContext),
		)
	}
	if domain.BoolFrom(flags["dialogue_only"]) {
		lines = append(lines,
			"",
			"DIALOGUE-ONLY MODE",
			"Rockstar AI owns all movement, tasks, and animations for this NPC.",
			"Do not request movement, combat, interaction, or attention tools.",
			"Only produce speech related to the current quest.",
			"If you have nothing relevant to say, return speech=null.",
		)
	}
	lines = append(lines, "", "CURRENT WORLD STATE", prettyJSON(world))
	goalText := "(none)"
	if goal, ok := currentGoal.(string); ok && goal != "" {
		goalText = goal
	} else if goal, ok := currentGoal.(*string); ok && goal != nil && *goal != "" {
		goalText = *goal
	}
	lines = append(lines,
		"",
		"CURRENT GOAL",
		goalText,
		"",
		"CURRENT MOOD",
		mood,
		"",
		"RECENT EVENTS",
		prettyJSON(recent),
	)
	if len(retrieved) > 0 {
		lines = append(lines, "", "RETRIEVED TIMELINE EVENTS", prettyJSON(retrieved))
	}
	availableTools := "(none)"
	if len(tools) > 0 {
		availableTools = strings.Join(tools, ", ")
	}
	lines = append(lines,
		"",
		"AVAILABLE TOOLS",
		availableTools,
		"",
		"ACTION ARGUMENT RULES",
		`- look_at / face / follow / flee_from require entity; use "player" for the player.`,
		"- go_to requires a destination string or position object.",
		"- wander requires a numeric radius.",
		"- investigate requires a position array [x, y, z].",
		"- wait requires a duration in seconds.",
		"- say requires non-empty text.",
		"",
		"Respond with a structured decision containing internal goal/mood, "+
			"optional speech, and a list of high-level tool actions.",
	)
	return strings.Join(lines, "\n")
}

// mapsOf normalizes the several slice shapes an AgentContext can carry after
// “ToMap“ (which produces []map[string]any) or after a raw map is passed in.
func mapsOf(value any) []map[string]any {
	switch typed := value.(type) {
	case nil:
		return nil
	case []map[string]any:
		return typed
	case []any:
		return domain.MapsFrom(typed)
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Array {
		return nil
	}
	out := make([]map[string]any, 0, reflected.Len())
	for index := 0; index < reflected.Len(); index++ {
		out = append(out, domain.MapFrom(reflected.Index(index).Interface()))
	}
	return out
}

// stringsOf normalizes []string and []any tool lists.
func stringsOf(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		return typed
	case []any:
		return domain.StringsFrom(typed)
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Array {
		return nil
	}
	out := make([]string, 0, reflected.Len())
	for index := 0; index < reflected.Len(); index++ {
		out = append(out, domain.StringFrom(reflected.Index(index).Interface()))
	}
	return out
}

// prettyJSON mirrors Python's “json.dumps(value, ensure_ascii=False,
// indent=2, sort_keys=True)“: two-space indentation, object keys sorted by
// code point, non-ASCII text preserved literally, and floats rendered with
// Python's “repr“ formatting rules. Values that cannot be represented fall
// back to “fmt“ formatting (Python falls back to “repr“ on TypeError).
func prettyJSON(value any) string {
	var builder strings.Builder
	writeJSON(&builder, value, 0)
	return builder.String()
}

func writeJSON(builder *strings.Builder, value any, depth int) {
	switch typed := value.(type) {
	case nil:
		builder.WriteString("null")
	case bool:
		if typed {
			builder.WriteString("true")
		} else {
			builder.WriteString("false")
		}
	case string:
		writeJSONString(builder, typed)
	case float64:
		builder.WriteString(pythonFloat(typed))
	case float32:
		builder.WriteString(pythonFloat(float64(typed)))
	case int:
		builder.WriteString(strconv.Itoa(typed))
	case int64:
		builder.WriteString(strconv.FormatInt(typed, 10))
	case int32:
		builder.WriteString(strconv.FormatInt(int64(typed), 10))
	case int16:
		builder.WriteString(strconv.FormatInt(int64(typed), 10))
	case int8:
		builder.WriteString(strconv.FormatInt(int64(typed), 10))
	case uint:
		builder.WriteString(strconv.FormatUint(uint64(typed), 10))
	case uint64:
		builder.WriteString(strconv.FormatUint(typed, 10))
	case uint32:
		builder.WriteString(strconv.FormatUint(uint64(typed), 10))
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			builder.WriteString(strconv.FormatInt(integer, 10))
			return
		}
		if number, err := typed.Float64(); err == nil {
			builder.WriteString(pythonFloat(number))
			return
		}
		writeJSONString(builder, typed.String())
	case []string:
		writeJSONSlice(builder, len(typed), depth, func(index int) { writeJSON(builder, typed[index], depth+2) })
	case []any:
		writeJSONSlice(builder, len(typed), depth, func(index int) { writeJSON(builder, typed[index], depth+2) })
	case []map[string]any:
		writeJSONSlice(builder, len(typed), depth, func(index int) { writeJSON(builder, typed[index], depth+2) })
	case map[string]any:
		writeJSONMap(builder, len(typed), depth, func(yield func(key string, value any)) {
			for _, key := range sortedKeys(typed) {
				yield(key, typed[key])
			}
		})
	case map[string]string:
		writeJSONMap(builder, len(typed), depth, func(yield func(key string, value any)) {
			for _, key := range sortedKeys(typed) {
				yield(key, typed[key])
			}
		})
	default:
		writeJSONReflect(builder, value, depth)
	}
}

func writeJSONReflect(builder *strings.Builder, value any, depth int) {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Pointer, reflect.Interface:
		if reflected.IsNil() {
			builder.WriteString("null")
			return
		}
		writeJSONReflect(builder, reflected.Elem().Interface(), depth)
	case reflect.Map:
		if reflected.Type().Key().Kind() != reflect.String {
			builder.WriteString(fmt.Sprintf("%v", value))
			return
		}
		keys := make([]string, 0, reflected.Len())
		for _, key := range reflected.MapKeys() {
			keys = append(keys, key.String())
		}
		sort.Strings(keys)
		writeJSONMap(builder, len(keys), depth, func(yield func(key string, value any)) {
			for _, key := range keys {
				yield(key, reflected.MapIndex(reflect.ValueOf(key)).Interface())
			}
		})
	case reflect.Slice, reflect.Array:
		if reflected.Kind() == reflect.Slice && reflected.IsNil() {
			builder.WriteString("[]")
			return
		}
		length := reflected.Len()
		writeJSONSlice(builder, length, depth, func(index int) {
			writeJSON(builder, reflected.Index(index).Interface(), depth+2)
		})
	default:
		builder.WriteString(fmt.Sprintf("%v", value))
	}
}

func writeJSONMap(builder *strings.Builder, size, depth int, each func(yield func(key string, value any))) {
	if size == 0 {
		builder.WriteString("{}")
		return
	}
	builder.WriteString("{\n")
	index := 0
	each(func(key string, value any) {
		builder.WriteString(indent(depth + 2))
		writeJSONString(builder, key)
		builder.WriteString(": ")
		writeJSON(builder, value, depth+2)
		if index < size-1 {
			builder.WriteByte(',')
		}
		builder.WriteByte('\n')
		index++
	})
	builder.WriteString(indent(depth))
	builder.WriteByte('}')
}

func writeJSONSlice(builder *strings.Builder, size, depth int, each func(index int)) {
	if size == 0 {
		builder.WriteString("[]")
		return
	}
	builder.WriteString("[\n")
	for index := 0; index < size; index++ {
		builder.WriteString(indent(depth + 2))
		each(index)
		if index < size-1 {
			builder.WriteByte(',')
		}
		builder.WriteByte('\n')
	}
	builder.WriteString(indent(depth))
	builder.WriteByte(']')
}

func indent(depth int) string {
	return strings.Repeat(" ", depth)
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// writeJSONString mirrors Python's “json.dumps(..., ensure_ascii=False)“
// string escaping: only quote, backslash, and C0 control characters are
// escaped; every other code point is emitted as literal UTF-8.
func writeJSONString(builder *strings.Builder, value string) {
	builder.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		case '\t':
			builder.WriteString(`\t`)
		case '\b':
			builder.WriteString(`\b`)
		case '\f':
			builder.WriteString(`\f`)
		default:
			if r < 0x20 {
				builder.WriteString(fmt.Sprintf(`\u%04x`, r))
				continue
			}
			builder.WriteRune(r)
		}
	}
	builder.WriteByte('"')
}

// pythonFloat renders a float the way Python's “repr“ does, because
// “json.dumps“ delegates to it: fixed notation for exponents in
// [-4, 16), scientific notation with a signed, at-least-two-digit exponent
// otherwise, and a trailing “.0“ on integral values.
func pythonFloat(value float64) string {
	if math.IsNaN(value) {
		return "NaN"
	}
	if math.IsInf(value, 1) {
		return "Infinity"
	}
	if math.IsInf(value, -1) {
		return "-Infinity"
	}
	scientific := strconv.FormatFloat(value, 'e', -1, 64)
	mantissa, exponentText, found := strings.Cut(scientific, "e")
	if !found {
		return scientific
	}
	exponent, err := strconv.Atoi(exponentText)
	if err != nil {
		return scientific
	}
	sign := ""
	if strings.HasPrefix(mantissa, "-") {
		sign = "-"
		mantissa = mantissa[1:]
	}
	digits := strings.Replace(mantissa, ".", "", 1)
	if exponent >= -4 && exponent < 16 {
		if exponent >= 0 {
			if len(digits) <= exponent+1 {
				return sign + digits + strings.Repeat("0", exponent+1-len(digits)) + ".0"
			}
			return sign + digits[:exponent+1] + "." + digits[exponent+1:]
		}
		return sign + "0." + strings.Repeat("0", -exponent-1) + digits
	}
	exponentSign := "+"
	if exponent < 0 {
		exponentSign = "-"
		exponent = -exponent
	}
	exponentDigits := strconv.Itoa(exponent)
	if len(exponentDigits) < 2 {
		exponentDigits = "0" + exponentDigits
	}
	if len(digits) == 1 {
		return sign + digits + "e" + exponentSign + exponentDigits
	}
	return sign + digits[:1] + "." + digits[1:] + "e" + exponentSign + exponentDigits
}
