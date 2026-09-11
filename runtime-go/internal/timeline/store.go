// Package timeline implements the append-only JSONL event timeline: the
// runtime's only long-term memory.
//
// It is a faithful port of `runtime/timeline/store.py`. The file format is
// shared with the Python runtime, so every line is written the way Python's
// `json.dumps(..., ensure_ascii=False, separators=(",", ":"))` writes it
// (compact separators, raw UTF-8, no HTML escaping) and read back through the
// canonical `domain.Event` decoding.
package timeline

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

const (
	// tailChunkSize matches the Python reader's 64 KiB backwards read size.
	tailChunkSize = 64 * 1024
	// maxLineBytes bounds a single JSONL line so a corrupt file cannot make a
	// reader allocate without limit. Python reads lines without a cap; a
	// timeline line is a few hundred bytes, so this is only a safety net.
	maxLineBytes = 16 * 1024 * 1024
)

// AppendOptions carries the optional fields of `append_event`. Every field
// is optional, exactly like the keyword-only arguments it mirrors.
type AppendOptions struct {
	// Data is the free-form, fact-shaped payload of the event.
	Data map[string]any
	// Entities lists the ids the event is about.
	Entities []string
	// Tags are the retrieval tags of the event.
	Tags []string
	// Summary is an optional human-readable one-liner.
	Summary *string
	// Importance is an optional score, clamped to [0, 1] on append.
	Importance *float64
	// GameTime is the optional in-game clock.
	GameTime *domain.GameTime
	// Location is the optional world position/region.
	Location *domain.Location
	// Timestamp overrides the automatically generated Unix timestamp.
	Timestamp *float64
}

// GrabFilter carries the deterministic retrieval filters of `grab_timeline`.
//
// Filters compose with AND. Multiple values passed to EventNames or Tags
// compose with OR within that field, matching the Python implementation.
type GrabFilter struct {
	// EventName adds one exact event name to the name filter.
	EventName string
	// EventNames adds several exact event names to the name filter.
	EventNames []string
	// Tag adds one required tag to the tag filter.
	Tag string
	// Tags adds several accepted tags to the tag filter (OR).
	Tags []string
	// Entity requires the event to mention this entity.
	Entity string
	// MinimumImportance requires a score at least this high; events without an
	// importance are rejected, as in Python.
	MinimumImportance *float64
	// SinceSeq keeps only events with a strictly larger sequence number.
	SinceSeq *int
	// BeforeSeq keeps only events with a strictly smaller sequence number.
	BeforeSeq *int
	// Limit keeps only the last Limit matches. A value <= 0 means "no
	// truncation", mirroring Python.
	Limit int
}

// Store is an append-only, per-NPC JSONL timeline store.
//
// A Store is safe for concurrent use: appends, sequence assignment, and reads
// are serialised internally. The Python original is single-threaded; the Go
// runtime serves one goroutine per bridge connection, so the lock keeps the
// sequence cache and the file consistent under that concurrency.
type Store struct {
	root string

	mu      sync.Mutex
	lastSeq map[string]int
}

// NewStore opens (and creates, including parents) the timeline directory.
func NewStore(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o777); err != nil {
		return nil, fmt.Errorf("create timeline root: %w", err)
	}
	return &Store{root: root, lastSeq: map[string]int{}}, nil
}

// PathFor returns the JSONL path of one NPC, sanitising the identifier exactly
// like Python: characters that are not alphanumeric (Unicode letters and
// numbers) or in "-_." become "_", and an identifier that is empty, "." or ".."
// is rejected.
func (s *Store) PathFor(npcID string) (string, error) {
	var safe strings.Builder
	for _, char := range npcID {
		if unicode.IsLetter(char) || unicode.IsNumber(char) ||
			char == '-' || char == '_' || char == '.' {
			safe.WriteRune(char)
			continue
		}
		safe.WriteRune('_')
	}
	name := safe.String()
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("invalid npc_id: %q", npcID)
	}
	return filepath.Join(s.root, name+".jsonl"), nil
}

// NextSeq returns the sequence number the next appended event will receive.
//
// The exported signature cannot report an error, so an unreadable or corrupt
// timeline yields 0 here; Append and AppendEvent report the same condition as
// an error and refuse to write.
func (s *Store) NextSeq(npcID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	seq, err := s.nextSeqLocked(npcID)
	if err != nil {
		return 0
	}
	return seq
}

// Append writes one event, repairing a wrong sequence number so the timeline
// stays linear. The returned event is the one that was actually written.
func (s *Store) Append(event domain.Event) (domain.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendLocked(event)
}

// AppendEvent creates and appends a new event in one call.
func (s *Store) AppendEvent(npcID, eventName string, opts AppendOptions) (domain.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Python computes the sequence before building the event, so an invalid
	// npc_id is reported ahead of an invalid event name.
	seq, err := s.nextSeqLocked(npcID)
	if err != nil {
		return domain.Event{}, err
	}

	data := make(map[string]any, len(opts.Data))
	for key, value := range opts.Data {
		data[key] = value
	}
	entities := append([]string{}, opts.Entities...)
	tags := append([]string{}, opts.Tags...)

	event := domain.NewEvent(domain.NewEventOptions{
		Seq:        seq,
		EventName:  eventName,
		NPCID:      npcID,
		Data:       data,
		Entities:   entities,
		Tags:       tags,
		Summary:    opts.Summary,
		Importance: opts.Importance,
		GameTime:   opts.GameTime,
		Location:   opts.Location,
		Timestamp:  opts.Timestamp,
	})
	return s.appendLocked(event)
}

// AllEvents reads every event of one NPC in file order. A missing timeline is
// an empty timeline, not an error.
func (s *Store) AllEvents(npcID string) ([]domain.Event, error) {
	path, err := s.PathFor(npcID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return readEvents(path)
}

// RecentEvents returns the last limit events by reading backwards from the end
// of the file; it never scans the whole timeline. A limit <= 0 returns no
// events, matching Python.
func (s *Store) RecentEvents(npcID string, limit int) ([]domain.Event, error) {
	if limit <= 0 {
		return []domain.Event{}, nil
	}
	path, err := s.PathFor(npcID)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	lines, err := tailNonEmptyLines(path, limit)
	if err != nil {
		return nil, err
	}
	events := make([]domain.Event, 0, len(lines))
	for _, line := range lines {
		// `recent_events` names the file, not the physical line.
		event, err := decodeEventLine(path, line)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

// GrabTimeline returns the events matching filter in file order, truncated to
// the last filter.Limit matches when Limit is positive.
func (s *Store) GrabTimeline(npcID string, filter GrabFilter) ([]domain.Event, error) {
	names := make(map[string]struct{}, len(filter.EventNames)+1)
	for _, name := range filter.EventNames {
		names[name] = struct{}{}
	}
	if filter.EventName != "" {
		names[filter.EventName] = struct{}{}
	}
	tagSet := make(map[string]struct{}, len(filter.Tags)+1)
	for _, tag := range filter.Tags {
		tagSet[tag] = struct{}{}
	}
	if filter.Tag != "" {
		tagSet[filter.Tag] = struct{}{}
	}

	events, err := s.AllEvents(npcID)
	if err != nil {
		return nil, err
	}

	matches := make([]domain.Event, 0, len(events))
	for _, event := range events {
		if len(names) > 0 {
			if _, ok := names[event.EventName]; !ok {
				continue
			}
		}
		if len(tagSet) > 0 && !intersects(tagSet, event.Tags) {
			continue
		}
		if filter.Entity != "" && !contains(event.Entities, filter.Entity) {
			continue
		}
		if filter.MinimumImportance != nil {
			if event.Importance == nil || *event.Importance < *filter.MinimumImportance {
				continue
			}
		}
		if filter.SinceSeq != nil && event.Seq <= *filter.SinceSeq {
			continue
		}
		if filter.BeforeSeq != nil && event.Seq >= *filter.BeforeSeq {
			continue
		}
		matches = append(matches, event)
	}

	if filter.Limit > 0 && len(matches) > filter.Limit {
		matches = matches[len(matches)-filter.Limit:]
	}
	return matches, nil
}

// HasTimeline reports whether one NPC has a timeline file. An invalid
// identifier reports false.
func (s *Store) HasTimeline(npcID string) bool {
	path, err := s.PathFor(npcID)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Delete removes one NPC's timeline. Deleting a timeline that does not exist
// is not an error; the cached sequence number is dropped either way.
func (s *Store) Delete(npcID string) error {
	path, err := s.PathFor(npcID)
	if err != nil {
		return err
	}
	if info, statErr := os.Lstat(path); statErr == nil && info.IsDir() {
		// Python's Path.unlink() refuses directories; os.Remove would remove
		// an empty one. Refuse as well so a stray directory is never lost.
		return fmt.Errorf("delete timeline %s: is a directory", path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	s.mu.Lock()
	delete(s.lastSeq, npcID)
	s.mu.Unlock()
	return nil
}

// appendLocked implements `append`. Callers must hold s.mu.
func (s *Store) appendLocked(event domain.Event) (domain.Event, error) {
	// Mirrors `next_seq` -> `path_for`, which rejects a bad npc_id first.
	path, err := s.PathFor(event.NPCID)
	if err != nil {
		return domain.Event{}, err
	}
	expected, err := s.nextSeqLocked(event.NPCID)
	if err != nil {
		return domain.Event{}, err
	}

	if event.Seq != expected {
		// Replace the sequence number and re-validate, exactly like
		// `Event.from_dict({**event.to_dict(), "seq": expected})`.
		payload := event.ToMap()
		payload["seq"] = expected
		repaired, err := domain.EventFromMap(payload)
		if err != nil {
			return domain.Event{}, fmt.Errorf("repair event seq: %w", err)
		}
		event = repaired
	} else if event.EventName == "" {
		// Python cannot build an Event without a name; guard Go's zero value
		// so an unnamed event never reaches a shared timeline.
		return domain.Event{}, errors.New("event_name is required")
	}

	line, err := encodeEventLine(event)
	if err != nil {
		return domain.Event{}, err
	}

	// Recreate the directory if it vanished (e.g. a temp timeline dir was
	// cleaned up while a disconnect handler was still writing).
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return domain.Event{}, fmt.Errorf("create timeline dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return domain.Event{}, fmt.Errorf("append timeline %s: %w", path, err)
	}
	if _, err := file.Write(line); err != nil {
		file.Close()
		return domain.Event{}, fmt.Errorf("append timeline %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return domain.Event{}, fmt.Errorf("append timeline %s: %w", path, err)
	}

	s.lastSeq[event.NPCID] = event.Seq
	return event, nil
}

// nextSeqLocked returns the next sequence number for one NPC, scanning the
// file once when the identifier has not been seen yet. Callers must hold s.mu.
func (s *Store) nextSeqLocked(npcID string) (int, error) {
	if last, ok := s.lastSeq[npcID]; ok {
		return last + 1, nil
	}
	path, err := s.PathFor(npcID)
	if err != nil {
		return 0, err
	}
	last := 0
	if _, err := os.Stat(path); err == nil {
		events, err := readEvents(path)
		if err != nil {
			return 0, err
		}
		for _, event := range events {
			if event.Seq > last {
				last = event.Seq
			}
		}
	}
	s.lastSeq[npcID] = last
	return last + 1, nil
}

// readEvents mirrors `iter_events`: it skips blank lines and turns a corrupt
// line into an error instead of a panic.
func readEvents(path string) ([]domain.Event, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []domain.Event{}, nil
		}
		return nil, err
	}
	defer file.Close()

	events := []domain.Event{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		event, err := decodeEventLine(fmt.Sprintf("%s:%d", path, lineNo), line)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read timeline %s: %w", path, err)
	}
	return events, nil
}

// decodeEventLine decodes one JSONL line. location identifies the line, either
// "path:line" like `iter_events` or just the path like `recent_events`.
func decodeEventLine(location string, line []byte) (domain.Event, error) {
	var payload map[string]any
	if err := json.Unmarshal(line, &payload); err != nil {
		return domain.Event{}, fmt.Errorf("invalid timeline line %s: %w", location, err)
	}
	event, err := domain.EventFromMap(payload)
	if err != nil {
		return domain.Event{}, fmt.Errorf("invalid timeline line %s: %w", location, err)
	}
	return event, nil
}

// tailNonEmptyLines reads the last limit non-empty lines without scanning the
// whole file, mirroring Python's `_tail_nonempty_lines` byte for byte.
func tailNonEmptyLines(path string, limit int) ([][]byte, error) {
	if limit <= 0 {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	position := info.Size()
	var data []byte
	newlineCount := 0
	for position > 0 && newlineCount <= limit {
		readSize := int64(tailChunkSize)
		if position < readSize {
			readSize = position
		}
		position -= readSize
		chunk := make([]byte, readSize)
		if _, err := file.ReadAt(chunk, position); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		data = append(chunk, data...)
		newlineCount = bytes.Count(data, []byte("\n"))
	}

	lines := bytes.Split(data, []byte("\n"))
	if position > 0 && len(lines) > 0 {
		// The first line may be partial because the read started in the middle
		// of the file.
		lines = lines[1:]
	}
	nonEmpty := make([][]byte, 0, len(lines))
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) > 0 {
			nonEmpty = append(nonEmpty, line)
		}
	}
	if len(nonEmpty) > limit {
		nonEmpty = nonEmpty[len(nonEmpty)-limit:]
	}
	return nonEmpty, nil
}

// encodeEventLine marshals an event exactly like the Python writer: compact
// separators, raw UTF-8 (`ensure_ascii=False`) and no HTML escaping. The
// returned bytes already end with the JSONL newline.
func encodeEventLine(event domain.Event) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(event.ToMap()); err != nil {
		return nil, fmt.Errorf("encode event: %w", err)
	}
	return buffer.Bytes(), nil
}

// intersects reports whether any tag is present in the set.
func intersects(set map[string]struct{}, values []string) bool {
	for _, value := range values {
		if _, ok := set[value]; ok {
			return true
		}
	}
	return false
}

// contains reports whether values holds one exact string.
func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
