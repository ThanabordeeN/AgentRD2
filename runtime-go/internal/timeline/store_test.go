package timeline

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

// newTestStore returns a store rooted in a per-test temporary directory.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func mustAppend(t *testing.T, store *Store, npcID, eventName string, opts AppendOptions) domain.Event {
	t.Helper()
	event, err := store.AppendEvent(npcID, eventName, opts)
	if err != nil {
		t.Fatalf("AppendEvent(%q, %q): %v", npcID, eventName, err)
	}
	return event
}

// TestAppendAssignsLinearSequence mirrors test_timeline.py's sequence test: the
// counter is per NPC and is repaired across a restart.
func TestAppendAssignsLinearSequence(t *testing.T) {
	store := newTestStore(t)

	first := mustAppend(t, store, "npc_1", "NPC_ACTIVATED", AppendOptions{Tags: []string{"npc"}})
	second := mustAppend(t, store, "npc_1", "PLAYER_APPROACHED", AppendOptions{Tags: []string{"player"}})
	other := mustAppend(t, store, "npc_2", "NPC_ACTIVATED", AppendOptions{Tags: []string{"npc"}})

	if got := []int{first.Seq, second.Seq, other.Seq}; fmt.Sprint(got) != fmt.Sprint([]int{1, 2, 1}) {
		t.Fatalf("sequence numbers = %v, want [1 2 1]", got)
	}

	reopened, err := NewStore(store.root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if got := reopened.NextSeq("npc_1"); got != 3 {
		t.Fatalf("NextSeq after reopen = %d, want 3", got)
	}

	events, err := store.AllEvents("npc_1")
	if err != nil {
		t.Fatalf("AllEvents: %v", err)
	}
	if len(events) != 2 || events[0].Seq != 1 || events[1].Seq != 2 {
		t.Fatalf("AllEvents = %+v, want seq [1 2]", events)
	}
}

// TestAppendRepairsWrongSeq covers the sequence-repair branch: a caller-supplied
// sequence is replaced by the next number, and the written event is returned.
func TestAppendRepairsWrongSeq(t *testing.T) {
	store := newTestStore(t)
	mustAppend(t, store, "npc_1", "FIRST", AppendOptions{})

	event := domain.NewEvent(domain.NewEventOptions{
		Seq:       99,
		EventName: "SECOND",
		NPCID:     "npc_1",
		Entities:  []string{"player"},
		Tags:      []string{"t"},
		Data:      map[string]any{"kept": true},
		EventID:   "evt_fixed_seq",
	})
	written, err := store.Append(event)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if written.Seq != 2 {
		t.Fatalf("repaired seq = %d, want 2", written.Seq)
	}
	if written.EventID != "evt_fixed_seq" {
		t.Fatalf("repair regenerated event_id = %q, want evt_fixed_seq", written.EventID)
	}
	if written.Data["kept"] != true {
		t.Fatalf("repair dropped data: %+v", written.Data)
	}

	events, err := store.AllEvents("npc_1")
	if err != nil {
		t.Fatalf("AllEvents: %v", err)
	}
	if len(events) != 2 || events[1].Seq != 2 || events[1].EventName != "SECOND" {
		t.Fatalf("timeline = %+v, want repaired second event", events)
	}
}

// TestAppendRepairsZeroSeq checks that Go's zero value is repaired rather than
// rejected, which is the only observable form of "wrong seq" a Go caller can
// pass.
func TestAppendRepairsZeroSeq(t *testing.T) {
	store := newTestStore(t)
	written, err := store.Append(domain.Event{EventName: "ONLY", NPCID: "npc_1"})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if written.Seq != 1 {
		t.Fatalf("seq = %d, want 1", written.Seq)
	}
}

// TestAppendRejectsInvalidEvents mirrors the ValueError raised by Python's
// Event invariants.
func TestAppendRejectsInvalidEvents(t *testing.T) {
	store := newTestStore(t)
	tests := []struct {
		name  string
		event domain.Event
		want  string
	}{
		{"empty npc", domain.Event{Seq: 1, EventName: "A"}, "invalid npc_id"},
		{"dot npc", domain.Event{Seq: 1, EventName: "A", NPCID: ".."}, "invalid npc_id"},
		{"empty name", domain.Event{Seq: 1, NPCID: "npc_1"}, "event_name is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.Append(test.event); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Append error = %v, want containing %q", err, test.want)
			}
		})
	}
	if _, err := store.AppendEvent("npc_1", "", AppendOptions{}); err == nil {
		t.Fatal("AppendEvent with an empty event name did not fail")
	}
}

// TestAppendEventClampsImportance mirrors Event.__post_init__.
func TestAppendEventClampsImportance(t *testing.T) {
	store := newTestStore(t)
	high := 1.5
	low := -2.0
	if got := mustAppend(t, store, "npc_1", "HIGH", AppendOptions{Importance: &high}).Importance; got == nil || *got != 1 {
		t.Fatalf("importance = %v, want 1", got)
	}
	if got := mustAppend(t, store, "npc_1", "LOW", AppendOptions{Importance: &low}).Importance; got == nil || *got != 0 {
		t.Fatalf("importance = %v, want 0", got)
	}
	if got := mustAppend(t, store, "npc_1", "NONE", AppendOptions{}).Importance; got != nil {
		t.Fatalf("importance = %v, want nil", got)
	}
}

// TestAppendRecreatesAVanishedDirectory mirrors the Python regression test for
// a disconnect handler that writes after the temp timeline dir was removed.
func TestAppendRecreatesAVanishedDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "timelines")
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mustAppend(t, store, "npc_1", "NPC_ACTIVATED", AppendOptions{})

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(root, entry.Name())); err != nil {
			t.Fatalf("remove %s: %v", entry.Name(), err)
		}
	}
	if err := os.Remove(root); err != nil {
		t.Fatalf("remove root: %v", err)
	}

	event := mustAppend(t, store, "npc_1", "NPC_RELEASED", AppendOptions{})
	if event.EventName != "NPC_RELEASED" {
		t.Fatalf("event name = %q", event.EventName)
	}
	if !store.HasTimeline("npc_1") {
		t.Fatal("timeline file was not recreated")
	}
}

// TestPathForSanitisesNPCID is the table of filename-sanitisation rules.
func TestPathForSanitisesNPCID(t *testing.T) {
	store := newTestStore(t)
	tests := []struct {
		npcID string
		want  string
	}{
		{"npc_1", "npc_1.jsonl"},
		{"npc 1", "npc_1.jsonl"},
		{"npc/2", "npc_2.jsonl"},
		{"npc:2", "npc_2.jsonl"},
		{"a/../../etc/passwd", "a_.._.._etc_passwd.jsonl"},
		{"npc.1-x_y", "npc.1-x_y.jsonl"},
		{"Elias Carter", "Elias_Carter.jsonl"},
		{"npc-001", "npc-001.jsonl"},
	}
	for _, test := range tests {
		t.Run(test.npcID, func(t *testing.T) {
			got, err := store.PathFor(test.npcID)
			if err != nil {
				t.Fatalf("PathFor(%q): %v", test.npcID, err)
			}
			if filepath.Base(got) != test.want {
				t.Fatalf("PathFor(%q) = %q, want base %q", test.npcID, got, test.want)
			}
			if filepath.Dir(got) != filepath.Clean(store.root) {
				t.Fatalf("PathFor(%q) escaped the root: %q", test.npcID, got)
			}
		})
	}

	for _, bad := range []string{"", ".", ".."} {
		if _, err := store.PathFor(bad); err == nil {
			t.Fatalf("PathFor(%q) did not fail", bad)
		}
	}
}

// TestGrabTimelineFilters mirrors every assertion of the Python filter test,
// one subtest per filter.
func TestGrabTimelineFilters(t *testing.T) {
	store := newTestStore(t)
	mustAppend(t, store, "npc_1", "PLAYER_THREATENED_NPC", AppendOptions{
		Tags: []string{"player", "threat"}, Importance: ptr(0.9), Entities: []string{"player"},
	})
	mustAppend(t, store, "npc_1", "PLAYER_HELPED_NPC", AppendOptions{
		Tags: []string{"player", "help"}, Importance: ptr(0.8), Entities: []string{"player"},
	})
	mustAppend(t, store, "npc_1", "GUNSHOT_HEARD", AppendOptions{
		Tags: []string{"world"}, Importance: ptr(0.6),
	})

	tests := []struct {
		name   string
		filter GrabFilter
		want   []int
	}{
		{"by name", GrabFilter{EventName: "PLAYER_THREATENED_NPC"}, []int{1}},
		{"by tag", GrabFilter{Tags: []string{"player"}}, []int{1, 2}},
		{"by tag union", GrabFilter{Tags: []string{"world", "help"}}, []int{2, 3}},
		{"minimum importance", GrabFilter{MinimumImportance: ptr(0.75)}, []int{1, 2}},
		{"by entity", GrabFilter{Entity: "player"}, []int{1, 2}},
		{"since seq", GrabFilter{SinceSeq: ptr(1)}, []int{2, 3}},
		{"before seq", GrabFilter{BeforeSeq: ptr(3)}, []int{1, 2}},
		{"limit keeps the tail", GrabFilter{Limit: 1}, []int{3}},
		{"limit zero means no truncation", GrabFilter{Limit: 0}, []int{1, 2, 3}},
		{"composed filters", GrabFilter{Tags: []string{"player"}, MinimumImportance: ptr(0.85)}, []int{1}},
		{"no match", GrabFilter{EventName: "MISSING"}, []int{}},
		{"importance rejects absent", GrabFilter{MinimumImportance: ptr(0.0)}, []int{1, 2, 3}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events, err := store.GrabTimeline("npc_1", test.filter)
			if err != nil {
				t.Fatalf("GrabTimeline: %v", err)
			}
			got := make([]int, 0, len(events))
			for _, event := range events {
				got = append(got, event.Seq)
			}
			if fmt.Sprint(got) != fmt.Sprint(test.want) {
				t.Fatalf("seqs = %v, want %v", got, test.want)
			}
		})
	}
}

// TestGrabTimelineEntityAndNameUnion checks OR-within-a-field semantics.
func TestGrabTimelineEntityAndNameUnion(t *testing.T) {
	store := newTestStore(t)
	mustAppend(t, store, "npc_1", "A_EVENT", AppendOptions{})
	mustAppend(t, store, "npc_1", "B_EVENT", AppendOptions{})
	mustAppend(t, store, "npc_1", "C_EVENT", AppendOptions{})

	events, err := store.GrabTimeline("npc_1", GrabFilter{EventNames: []string{"A_EVENT", "C_EVENT"}})
	if err != nil {
		t.Fatalf("GrabTimeline: %v", err)
	}
	if len(events) != 2 || events[0].EventName != "A_EVENT" || events[1].EventName != "C_EVENT" {
		t.Fatalf("events = %+v, want A_EVENT and C_EVENT", events)
	}
}

// TestRecentEventsReturnsTail mirrors the Python tail test.
func TestRecentEventsReturnsTail(t *testing.T) {
	store := newTestStore(t)
	for i := 0; i < 5; i++ {
		mustAppend(t, store, "npc_1", "EVENT", AppendOptions{Data: map[string]any{"i": i}})
	}
	recent, err := store.RecentEvents("npc_1", 2)
	if err != nil {
		t.Fatalf("RecentEvents: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("recent = %d events, want 2", len(recent))
	}
	for index, want := range []int{3, 4} {
		if got := fmt.Sprint(recent[index].Data["i"]); got != fmt.Sprint(want) {
			t.Fatalf("recent[%d].i = %v, want %d", index, recent[index].Data["i"], want)
		}
	}

	for _, limit := range []int{0, -1} {
		empty, err := store.RecentEvents("npc_1", limit)
		if err != nil {
			t.Fatalf("RecentEvents(%d): %v", limit, err)
		}
		if len(empty) != 0 {
			t.Fatalf("RecentEvents(%d) = %d events, want 0", limit, len(empty))
		}
	}
}

// TestRecentEventsSeeksFromTheEnd proves the reader starts at the end of the
// file: a corrupt head that is never read does not break the tail read, while
// the same file makes AllEvents fail.
func TestRecentEventsSeeksFromTheEnd(t *testing.T) {
	store := newTestStore(t)
	path, err := store.PathFor("npc_1")
	if err != nil {
		t.Fatalf("PathFor: %v", err)
	}
	var builder strings.Builder
	builder.WriteString("{this is not valid json}\n")
	// Enough well-formed lines to push the corrupt head outside the last 64 KiB
	// window the tail reader inspects.
	for i := 0; i < 1000; i++ {
		builder.WriteString(fmt.Sprintf(
			"{\"seq\":%d,\"event_id\":\"evt_%d\",\"event_name\":\"FILLER\",\"timestamp\":1.5,"+
				"\"npc_id\":\"npc_1\",\"entities\":[],\"data\":{\"padding\":\"%s\"},\"tags\":[]}\n",
			i+1, i, strings.Repeat("x", 64)))
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	recent, err := store.RecentEvents("npc_1", 1)
	if err != nil {
		t.Fatalf("RecentEvents: %v", err)
	}
	if len(recent) != 1 || recent[0].Seq != 1000 {
		t.Fatalf("recent = %+v, want the last filler event", recent)
	}

	if _, err := store.AllEvents("npc_1"); err == nil {
		t.Fatal("AllEvents accepted a corrupt head")
	}
}

// TestCorruptLineIsAnErrorNotAPanic covers every read path and both sequence
// cache states: a cold cache reports the corruption, a warm one never rescans
// (matching Python's _last_seq cache).
func TestCorruptLineIsAnErrorNotAPanic(t *testing.T) {
	store := newTestStore(t)
	mustAppend(t, store, "npc_1", "GOOD", AppendOptions{})
	path, err := store.PathFor("npc_1")
	if err != nil {
		t.Fatalf("PathFor: %v", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := file.WriteString("{not json\n"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	file.Close()

	if _, err := store.AllEvents("npc_1"); err == nil {
		t.Fatal("AllEvents accepted a corrupt line")
	} else if !strings.Contains(err.Error(), "invalid timeline line") || !strings.Contains(err.Error(), ":2:") {
		t.Fatalf("AllEvents error = %v, want an invalid-line error naming line 2", err)
	}
	if _, err := store.RecentEvents("npc_1", 5); err == nil {
		t.Fatal("RecentEvents accepted a corrupt line")
	}
	if _, err := store.GrabTimeline("npc_1", GrabFilter{}); err == nil {
		t.Fatal("GrabTimeline accepted a corrupt line")
	}

	// This store already knows the last sequence number, so it does not rescan
	// the corrupt line - the same shortcut Python takes.
	if got := store.NextSeq("npc_1"); got != 2 {
		t.Fatalf("warm NextSeq = %d, want 2 from the cache", got)
	}
	if _, err := store.AppendEvent("npc_1", "LATER", AppendOptions{}); err != nil {
		t.Fatalf("warm AppendEvent: %v", err)
	}

	// A cold cache must surface the corruption instead of guessing.
	cold, err := NewStore(store.root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if got := cold.NextSeq("npc_1"); got != 0 {
		// NextSeq cannot return an error; 0 is the documented fallback.
		t.Fatalf("cold NextSeq on a corrupt timeline = %d, want 0", got)
	}
	if _, err := cold.AppendEvent("npc_1", "COLD_LATER", AppendOptions{}); err == nil {
		t.Fatal("cold AppendEvent appended past a corrupt line")
	} else if !strings.Contains(err.Error(), "invalid timeline line") {
		t.Fatalf("cold AppendEvent error = %v, want an invalid-line error", err)
	}

	// The rejected append must not have reached the file.
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(content), "COLD_LATER") {
		t.Fatal("a rejected append reached the timeline")
	}
	if !strings.Contains(string(content), "LATER") {
		t.Fatal("the warm append did not reach the timeline")
	}
}

// TestHasTimelineAndDelete covers existence checks and deletion, including the
// sequence cache reset.
func TestHasTimelineAndDelete(t *testing.T) {
	store := newTestStore(t)
	if store.HasTimeline("npc_1") {
		t.Fatal("HasTimeline on a fresh store = true")
	}
	if err := store.Delete("npc_1"); err != nil {
		t.Fatalf("Delete on a missing timeline: %v", err)
	}
	mustAppend(t, store, "npc_1", "FIRST", AppendOptions{})
	if !store.HasTimeline("npc_1") {
		t.Fatal("HasTimeline after append = false")
	}
	if err := store.Delete("npc_1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if store.HasTimeline("npc_1") {
		t.Fatal("HasTimeline after delete = true")
	}
	if got := store.NextSeq("npc_1"); got != 1 {
		t.Fatalf("NextSeq after delete = %d, want 1", got)
	}
	if err := store.Delete("npc_1"); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
}

// TestDeleteOnMissingParentIsNotAnError keeps the FileNotFoundError branch.
func TestDeleteOnMissingParentIsNotAnError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "timelines")
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if err := store.Delete("npc_1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

// TestAppendWritesPythonCompatibleJSON pins the shared on-disk format:
// compact separators, raw UTF-8, and no HTML escaping.
func TestAppendWritesPythonCompatibleJSON(t *testing.T) {
	store := newTestStore(t)
	summary := "Elias a vu <le> joueur & «l'étranger»"
	mustAppend(t, store, "npc_1", "NPC_SPOKE", AppendOptions{
		Data:    map[string]any{"text": "Bonjour «monde» <b>"},
		Summary: &summary,
	})

	path, err := store.PathFor("npc_1")
	if err != nil {
		t.Fatalf("PathFor: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	line := string(raw)
	for _, want := range []string{"<le>", "&", "«l'étranger»", "Bonjour «monde» <b>"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q does not contain raw %q", line, want)
		}
	}
	if strings.Contains(line, `\u003c`) || strings.Contains(line, `\u00ab`) {
		t.Fatalf("line %q was escaped like Go's default encoder", line)
	}
	if strings.Contains(line, `": `) || strings.Contains(line, `", "`) {
		t.Fatalf("line %q is not compact", line)
	}
	if !strings.HasSuffix(line, "\n") {
		t.Fatalf("line %q does not end with a newline", line)
	}
	if strings.Count(line, "\n") != 1 {
		t.Fatalf("line %q holds more than one JSONL record", line)
	}

	events, err := store.AllEvents("npc_1")
	if err != nil {
		t.Fatalf("AllEvents: %v", err)
	}
	if len(events) != 1 || events[0].Summary == nil || *events[0].Summary != summary {
		t.Fatalf("round trip = %+v", events)
	}
}

// TestReadsPythonWrittenTimeline checks the other direction of the shared
// format: a line produced by the Python writer decodes into the canonical
// event, blank lines are skipped, and optional fields survive.
func TestReadsPythonWrittenTimeline(t *testing.T) {
	store := newTestStore(t)
	path, err := store.PathFor("npc_1")
	if err != nil {
		t.Fatalf("PathFor: %v", err)
	}
	lines := []string{
		`{"seq":1,"event_id":"evt_a","event_name":"NPC_ACTIVATED","timestamp":1700000000.25,` +
			`"npc_id":"npc_1","entities":["player"],"data":{"text":"héllo <world> & co"},` +
			`"tags":["npc"],"game_time":{"day":3,"hour":7,"minute":5},` +
			`"location":{"region":"Valentine","position":[1.5,2.25,3.0]},` +
			`"summary":"s","importance":0.75}`,
		"",
		`{"seq":2,"event_id":"evt_b","event_name":"PLAYER_SPOKE","timestamp":1700000001.5,` +
			`"npc_id":"npc_1","entities":[],"data":{},"tags":[]}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	events, err := store.AllEvents("npc_1")
	if err != nil {
		t.Fatalf("AllEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	first := events[0]
	if first.GameTime == nil || first.GameTime.Day != 3 || first.GameTime.Hour != 7 {
		t.Fatalf("game_time = %+v", first.GameTime)
	}
	if first.Location == nil || first.Location.Region == nil || *first.Location.Region != "Valentine" {
		t.Fatalf("location = %+v", first.Location)
	}
	if len(first.Location.Position) != 3 || first.Location.Position[1] != 2.25 {
		t.Fatalf("position = %v", first.Location.Position)
	}
	if first.Data["text"] != "héllo <world> & co" {
		t.Fatalf("data = %+v", first.Data)
	}
	if first.Importance == nil || *first.Importance != 0.75 {
		t.Fatalf("importance = %v", first.Importance)
	}
	if got := store.NextSeq("npc_1"); got != 3 {
		t.Fatalf("NextSeq = %d, want 3", got)
	}
}

// TestAppendEventCarriesOptionalFields covers the whole AppendOptions surface.
func TestAppendEventCarriesOptionalFields(t *testing.T) {
	store := newTestStore(t)
	summary := "held the door"
	importance := 0.4
	timestamp := 1234.5
	region := "Valentine"
	event := mustAppend(t, store, "npc_1", "PLAYER_HELPED_NPC", AppendOptions{
		Data:       map[string]any{"verb": "help"},
		Entities:   []string{"player", "npc_1"},
		Tags:       []string{"player", "help"},
		Summary:    &summary,
		Importance: &importance,
		GameTime:   &domain.GameTime{Day: 2, Hour: 9, Minute: 30},
		Location:   &domain.Location{Region: &region, Position: []float64{1, 2, 3}},
		Timestamp:  &timestamp,
	})

	if event.Timestamp != timestamp {
		t.Fatalf("timestamp = %v, want %v", event.Timestamp, timestamp)
	}
	if event.Summary == nil || *event.Summary != summary {
		t.Fatalf("summary = %v", event.Summary)
	}
	if len(event.Entities) != 2 || len(event.Tags) != 2 {
		t.Fatalf("entities/tags = %v/%v", event.Entities, event.Tags)
	}
	if event.GameTime == nil || event.GameTime.Minute != 30 {
		t.Fatalf("game_time = %+v", event.GameTime)
	}
	if event.Location == nil || event.Location.Region == nil || *event.Location.Region != region {
		t.Fatalf("location = %+v", event.Location)
	}

	// The caller's maps and slices must not alias the stored event.
	opts := AppendOptions{Data: map[string]any{"a": 1}, Entities: []string{"x"}, Tags: []string{"y"}}
	stored := mustAppend(t, store, "npc_1", "COPY", opts)
	opts.Data["a"] = 2
	opts.Entities[0] = "changed"
	opts.Tags[0] = "changed"
	if stored.Data["a"] != 1 || stored.Entities[0] != "x" || stored.Tags[0] != "y" {
		t.Fatalf("AppendEvent aliased caller state: %+v", stored)
	}
}

// TestConcurrentAppendsStayLinear exercises the internal lock: every write must
// get a distinct sequence number.
func TestConcurrentAppendsStayLinear(t *testing.T) {
	store := newTestStore(t)
	const writers = 8
	const perWriter = 10

	var group sync.WaitGroup
	errs := make(chan error, writers)
	for writer := 0; writer < writers; writer++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for i := 0; i < perWriter; i++ {
				if _, err := store.AppendEvent("npc_1", "EVENT", AppendOptions{Data: map[string]any{"i": i}}); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent append: %v", err)
	}

	events, err := store.AllEvents("npc_1")
	if err != nil {
		t.Fatalf("AllEvents: %v", err)
	}
	if len(events) != writers*perWriter {
		t.Fatalf("events = %d, want %d", len(events), writers*perWriter)
	}
	for index, event := range events {
		if event.Seq != index+1 {
			t.Fatalf("event %d has seq %d, want %d", index, event.Seq, index+1)
		}
	}
}

// TestNewStoreCreatesRoot covers construction of a nested directory.
func TestNewStoreCreatesRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "deep", "timelines")
	if _, err := NewStore(root); err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		t.Fatalf("root not created: %v", err)
	}
}

// TestPythonInteropRoundTrip shells out to the repository's Python runtime, when
// available, to prove both writers read each other's lines.
func TestPythonInteropRoundTrip(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}

	store := newTestStore(t)
	summary := "il a dit «bonjour» & <parti>"
	mustAppend(t, store, "npc_1", "NPC_SPOKE", AppendOptions{
		Data:    map[string]any{"text": "héllo"},
		Summary: &summary,
	})
	path, err := store.PathFor("npc_1")
	if err != nil {
		t.Fatalf("PathFor: %v", err)
	}

	script := `import json,sys
with open(sys.argv[1], encoding="utf-8") as fh:
    event = json.loads(fh.readline())
assert event["event_name"] == "NPC_SPOKE", event
assert event["npc_id"] == "npc_1", event
assert event["data"]["text"] == "héllo", event
assert event["summary"] == "il a dit «bonjour» & <parti>", event
assert isinstance(event["seq"], int) and event["entities"] == [] and event["tags"] == [], event
print(json.dumps({"seq": 9, "event_id": "evt_py", "event_name": "PYTHON_EVENT",
                  "timestamp": 42.5, "npc_id": "npc_1", "entities": ["player"],
                  "data": {"ok": True}, "tags": ["py"],
                  "game_time": {"day": 1, "hour": 2, "minute": 3}}, ensure_ascii=False,
                 separators=(",", ":")))
`
	output, err := exec.Command(python, "-c", script, path).CombinedOutput()
	if err != nil {
		t.Fatalf("python interop failed: %v\n%s", err, output)
	}

	line := strings.TrimSpace(string(output))
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := file.WriteString(line + "\n"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	file.Close()

	reopened, err := NewStore(store.root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	events, err := reopened.AllEvents("npc_1")
	if err != nil {
		t.Fatalf("AllEvents: %v", err)
	}
	if len(events) != 2 || events[1].EventName != "PYTHON_EVENT" || events[1].Seq != 9 {
		t.Fatalf("events = %+v", events)
	}
	if events[1].GameTime == nil || events[1].GameTime.Minute != 3 {
		t.Fatalf("game_time = %+v", events[1].GameTime)
	}
	if got := reopened.NextSeq("npc_1"); got != 10 {
		t.Fatalf("NextSeq = %d, want 10", got)
	}
}

// TestDecodeErrorSurfacesAsError guards the "no panic on corrupt input" rule
// with inputs that would panic a careless decoder.
func TestDecodeErrorSurfacesAsError(t *testing.T) {
	store := newTestStore(t)
	path, err := store.PathFor("npc_1")
	if err != nil {
		t.Fatalf("PathFor: %v", err)
	}
	for _, line := range []string{"null", "[]", `"text"`, "42", `{"seq":"not-a-number","event_name":"X","npc_id":"npc_1"}`} {
		if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if _, err := store.AllEvents("npc_1"); err == nil {
			t.Fatalf("AllEvents accepted %q", line)
		}
		if _, err := store.RecentEvents("npc_1", 1); err == nil {
			t.Fatalf("RecentEvents accepted %q", line)
		}
	}
}

// TestAppendDoesNotAliasEventDataAfterWrite makes sure the encoder consumes the
// same data the caller handed over without mutating it.
func TestAppendDoesNotAliasEventDataAfterWrite(t *testing.T) {
	store := newTestStore(t)
	data := map[string]any{"nested": map[string]any{"a": 1}}
	event := mustAppend(t, store, "npc_1", "EVENT", AppendOptions{Data: data})
	raw, err := json.Marshal(event.Data)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(raw) != `{"nested":{"a":1}}` {
		t.Fatalf("data = %s", raw)
	}
}

func ptr[T any](value T) *T {
	return &value
}
