package ipc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// logSink captures the server's status lines for assertions.
type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *logSink) Write(chunk []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(chunk)
}

func (s *logSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// waitFor polls until the log holds substr, mirroring the Python tests that
// wait on the flushed status lines.
func (s *logSink) waitFor(t *testing.T, substr string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if text := s.String(); strings.Contains(text, substr) {
			return text
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("log line %q never appeared; log:\n%s", substr, s.String())
	return ""
}

// waitForLineCount polls until a log line appears a given number of times.
func (s *logSink) waitForLineCount(t *testing.T, line string, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(s.String(), line) >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("log line %q appeared %d times, want %d; log:\n%s",
		line, strings.Count(s.String(), line), want, s.String())
}

// fakeHandler records bridge traffic and answers with canned replies.
type fakeHandler struct {
	mu       sync.Mutex
	messages []map[string]any
	releases []string

	reply       func(raw map[string]any) (map[string]any, error)
	handleErr   error
	handleDelay time.Duration
	releaseErr  error
}

func (h *fakeHandler) HandleMessage(ctx context.Context, raw map[string]any) (map[string]any, error) {
	if h.handleDelay > 0 {
		select {
		case <-time.After(h.handleDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	h.mu.Lock()
	h.messages = append(h.messages, raw)
	h.mu.Unlock()

	if h.handleErr != nil {
		return nil, h.handleErr
	}
	if h.reply != nil {
		return h.reply(raw)
	}
	return nil, nil
}

func (h *fakeHandler) ReleaseAll(_ context.Context, reason string) error {
	h.mu.Lock()
	h.releases = append(h.releases, reason)
	h.mu.Unlock()
	return h.releaseErr
}

func (h *fakeHandler) messageCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.messages)
}

func (h *fakeHandler) lastMessage() map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.messages) == 0 {
		return nil
	}
	return h.messages[len(h.messages)-1]
}

func (h *fakeHandler) releaseCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.releases)
}

// releaseReasons returns the reason of every ReleaseAll call.
func (h *fakeHandler) releaseReasons() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.releases...)
}

// assertReleaseReason pins the literal reason the Python server passes.
func (h *fakeHandler) assertReleaseReason(t *testing.T, want string) {
	t.Helper()
	for _, reason := range h.releaseReasons() {
		if reason != want {
			t.Fatalf("ReleaseAll reason = %q, want %q", reason, want)
		}
	}
}

func (h *fakeHandler) waitForReleases(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if h.releaseCount() >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("ReleaseAll calls = %d, want %d", h.releaseCount(), want)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// startServer binds one server on an ephemeral port and waits for the
// documented listening line, then returns the address it advertises.
func startServer(t *testing.T, handler Handler) (*Server, *logSink, string) {
	t.Helper()
	server := NewServer(handler, "127.0.0.1", 0)
	logs := &logSink{}
	server.logOutput = logs

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- server.Serve(ctx) }()

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-served:
			if err != nil {
				t.Errorf("Serve returned %v, want nil after cancellation", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("Serve did not return after cancellation")
		}
	})

	return server, logs, listeningAddress(t, logs)
}

// listeningAddress extracts the address from the listening line the way a
// bridge-side test would.
func listeningAddress(t *testing.T, logs *logSink) string {
	t.Helper()
	text := logs.waitFor(t, "[ipc] listening on ")
	for _, line := range strings.Split(text, "\n") {
		address, ok := strings.CutPrefix(line, "[ipc] listening on ")
		if !ok {
			continue
		}
		address = strings.TrimSpace(address)
		if address != "" && !strings.HasSuffix(address, ":0") {
			return address
		}
	}
	t.Fatalf("listening line did not carry a bound address; log:\n%s", text)
	return ""
}

func dial(t *testing.T, address string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", address, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, bufio.NewReader(conn)
}

// send writes one raw line to the server.
func send(t *testing.T, conn net.Conn, line string) {
	t.Helper()
	if err := conn.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("SetWriteDeadline: %v", err)
	}
	if _, err := conn.Write([]byte(line + "\n")); err != nil {
		t.Fatalf("write %q: %v", line, err)
	}
}

func sendJSON(t *testing.T, conn net.Conn, payload map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	send(t, conn, string(encoded))
}

// readJSON reads one protocol line, failing the test on EOF or bad JSON.
func readJSON(t *testing.T, reader *bufio.Reader) map[string]any {
	t.Helper()
	line, err := readLine(t, reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(line, &payload); err != nil {
		t.Fatalf("decode %q: %v", line, err)
	}
	return payload
}

// readLine reads one raw line with a deadline.
func readLine(t *testing.T, reader *bufio.Reader) ([]byte, error) {
	t.Helper()
	type result struct {
		line []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		line, err := reader.ReadBytes('\n')
		done <- result{line: line, err: err}
	}()
	select {
	case got := <-done:
		return got.line, got.err
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a protocol line")
		return nil, nil
	}
}

// waitForEOF asserts the server closed its side of the connection.
func waitForEOF(t *testing.T, conn net.Conn, reader *bufio.Reader) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("expected the connection to be closed")
	}
}

// waitForBridges waits until the server has registered count bridge
// connections. The server attaches the connection before it logs the
// "bridge connected" line, so the log count is a safe registration signal.
func waitForBridges(t *testing.T, logs *logSink, count int) {
	t.Helper()
	logs.waitForLineCount(t, "[ipc] bridge connected: ", count)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestServeDispatchesMessagesAndReplies is the happy path: a real TCP
// conversation with a reply for every handled message.
func TestServeDispatchesMessagesAndReplies(t *testing.T) {
	handler := &fakeHandler{reply: func(raw map[string]any) (map[string]any, error) {
		return map[string]any{"type": "hello_ack", "echo": raw["type"]}, nil
	}}
	server, logs, address := startServer(t, handler)

	if server.Address() != address {
		t.Fatalf("Address() = %q, want %q", server.Address(), address)
	}
	if strings.HasSuffix(address, ":0") {
		t.Fatalf("Address() = %q, want the bound port", address)
	}

	conn, reader := dial(t, address)
	waitForBridges(t, logs, 1)

	sendJSON(t, conn, map[string]any{"type": "hello"})
	reply := readJSON(t, reader)
	if reply["type"] != "hello_ack" || reply["echo"] != "hello" {
		t.Fatalf("reply = %+v", reply)
	}

	sendJSON(t, conn, map[string]any{"type": "ped_scan", "peds": []any{map[string]any{"entity_id": "npc_001"}}})
	reply = readJSON(t, reader)
	if reply["type"] != "hello_ack" || reply["echo"] != "ped_scan" {
		t.Fatalf("second reply = %+v", reply)
	}

	if got := handler.messageCount(); got != 2 {
		t.Fatalf("handled messages = %d, want 2", got)
	}
	last := handler.lastMessage()
	if last["type"] != "ped_scan" {
		t.Fatalf("last message = %+v", last)
	}
	peds, ok := last["peds"].([]any)
	if !ok || len(peds) != 1 {
		t.Fatalf("peds payload = %#v", last["peds"])
	}
}

// TestInvalidJSONAndNonObjectFrames pins both error frames of the protocol.
func TestInvalidJSONAndNonObjectFrames(t *testing.T) {
	handler := &fakeHandler{}
	_, _, address := startServer(t, handler)
	conn, reader := dial(t, address)

	tests := []struct {
		name   string
		line   string
		reason string
	}{
		{"truncated json", `{"type":`, "invalid json"},
		{"empty line", "", "invalid json"},
		{"whitespace line", `   `, "invalid json"},
		{"array", `[1,2,3]`, "message must be an object"},
		{"string scalar", `"text"`, "message must be an object"},
		{"number scalar", `42`, "message must be an object"},
		{"json null", `null`, "message must be an object"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			send(t, conn, test.line)
			frame := readJSON(t, reader)
			if frame["type"] != "error" || frame["reason"] != test.reason {
				t.Fatalf("frame = %+v, want reason %q", frame, test.reason)
			}
		})
	}

	if got := handler.messageCount(); got != 0 {
		t.Fatalf("handler saw %d messages, want 0", got)
	}
}

// TestHandleMessageErrorClosesTheConnection mirrors Python letting a handler
// exception escape the connection loop.
func TestHandleMessageErrorClosesTheConnection(t *testing.T) {
	handler := &fakeHandler{handleErr: fmt.Errorf("boom")}
	_, logs, address := startServer(t, handler)
	conn, reader := dial(t, address)

	sendJSON(t, conn, map[string]any{"type": "hello"})
	waitForEOF(t, conn, reader)
	logs.waitFor(t, "[ipc] bridge disconnected: ")
	handler.waitForReleases(t, 1)
}

// TestNilReplyWritesNothing distinguishes "handler returned no reply" from EOF:
// the next line on the wire must belong to the second message.
func TestNilReplyWritesNothing(t *testing.T) {
	handler := &fakeHandler{reply: func(raw map[string]any) (map[string]any, error) {
		if raw["type"] == "reply" {
			return map[string]any{"type": "ack"}, nil
		}
		return nil, nil
	}}
	_, _, address := startServer(t, handler)
	conn, reader := dial(t, address)

	sendJSON(t, conn, map[string]any{"type": "silent"})
	sendJSON(t, conn, map[string]any{"type": "reply"})
	frame := readJSON(t, reader)
	if frame["type"] != "ack" {
		t.Fatalf("frame = %+v, want the second message's reply", frame)
	}
	if got := handler.messageCount(); got != 2 {
		t.Fatalf("handled messages = %d, want 2", got)
	}
}

// TestSendWritesToTheBridge covers the dispatcher-facing send path.
func TestSendWritesToTheBridge(t *testing.T) {
	handler := &fakeHandler{}
	server, logs, address := startServer(t, handler)

	// No bridge yet: the Python send_json is a silent no-op.
	if err := server.Send("npc_1", map[string]any{"type": "action_request"}); err != nil {
		t.Fatalf("Send without a bridge: %v", err)
	}

	_, reader := dial(t, address)
	waitForBridges(t, logs, 1)

	for index := 0; index < 3; index++ {
		payload := map[string]any{
			"type":   "action_request",
			"npc_id": "npc_1",
			"index":  index,
		}
		if err := server.Send("npc_1", payload); err != nil {
			t.Fatalf("Send: %v", err)
		}
		frame := readJSON(t, reader)
		if frame["type"] != "action_request" || frame["npc_id"] != "npc_1" {
			t.Fatalf("frame = %+v", frame)
		}
		if got := fmt.Sprint(frame["index"]); got != fmt.Sprint(index) {
			t.Fatalf("index = %v, want %d", frame["index"], index)
		}
	}
}

// TestConcurrentSendsAreSerialised proves writes never interleave, which is what
// the Python event loop provided implicitly.
func TestConcurrentSendsAreSerialised(t *testing.T) {
	handler := &fakeHandler{}
	server, logs, address := startServer(t, handler)
	_, reader := dial(t, address)
	waitForBridges(t, logs, 1)

	const writers = 8
	const perWriter = 12
	var group sync.WaitGroup
	errs := make(chan error, writers)
	for writer := 0; writer < writers; writer++ {
		group.Add(1)
		go func(writer int) {
			defer group.Done()
			for index := 0; index < perWriter; index++ {
				payload := map[string]any{
					"type":   "action_request",
					"npc_id": fmt.Sprintf("npc_%d", writer),
					"marker": fmt.Sprintf("%d-%d", writer, index),
					"pad":    strings.Repeat("p", 64),
				}
				if err := server.Send("npc", payload); err != nil {
					errs <- err
					return
				}
			}
		}(writer)
	}
	group.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("Send: %v", err)
	}

	seen := map[string]bool{}
	for index := 0; index < writers*perWriter; index++ {
		frame := readJSON(t, reader)
		marker, _ := frame["marker"].(string)
		if marker == "" {
			t.Fatalf("frame %d lost its marker and was probably interleaved: %+v", index, frame)
		}
		if seen[marker] {
			t.Fatalf("duplicate marker %q", marker)
		}
		seen[marker] = true
	}
	if len(seen) != writers*perWriter {
		t.Fatalf("distinct frames = %d, want %d", len(seen), writers*perWriter)
	}
}

// TestDisconnectReleasesOwnership mirrors the spec release policy.
func TestDisconnectReleasesOwnership(t *testing.T) {
	handler := &fakeHandler{}
	_, logs, address := startServer(t, handler)

	conn, _ := dial(t, address)
	logs.waitFor(t, "[ipc] bridge connected: ")
	if handler.releaseCount() != 0 {
		t.Fatal("ReleaseAll ran while the bridge was connected")
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	handler.waitForReleases(t, 1)
	handler.assertReleaseReason(t, "bridge_disconnect")
	logs.waitFor(t, "[ipc] bridge disconnected: ")
}

// TestOlderConnectionDoesNotReleaseTheNewest mirrors Python's
// `if self._writer is writer` guard.
func TestOlderConnectionDoesNotReleaseTheNewest(t *testing.T) {
	handler := &fakeHandler{}
	server, logs, address := startServer(t, handler)

	older, _ := dial(t, address)
	waitForBridges(t, logs, 1)

	newer, newerReader := dial(t, address)
	waitForBridges(t, logs, 2)

	if err := older.Close(); err != nil {
		t.Fatalf("Close older: %v", err)
	}
	// The disconnect line is written after the release decision, so seeing it
	// proves the older connection did not release ownership.
	logs.waitForLineCount(t, "[ipc] bridge disconnected: ", 1)
	if got := handler.releaseCount(); got != 0 {
		t.Fatalf("ReleaseAll calls after an older disconnect = %d, want 0", got)
	}

	// The newest connection still owns the send path, and its own disconnect
	// releases ownership.
	if err := server.Send("npc", map[string]any{"type": "still_here"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	frame := readJSON(t, newerReader)
	if frame["type"] != "still_here" {
		t.Fatalf("frame = %+v", frame)
	}

	if err := newer.Close(); err != nil {
		t.Fatalf("Close newer: %v", err)
	}
	handler.waitForReleases(t, 1)
	handler.assertReleaseReason(t, "bridge_disconnect")
}

// TestReleaseAllErrorStillReportsDisconnect keeps the required log line even
// when the handler fails during release.
func TestReleaseAllErrorStillReportsDisconnect(t *testing.T) {
	handler := &fakeHandler{releaseErr: fmt.Errorf("release failed")}
	_, logs, address := startServer(t, handler)

	conn, _ := dial(t, address)
	logs.waitFor(t, "[ipc] bridge connected: ")
	conn.Close()
	logs.waitFor(t, "[ipc] bridge disconnected: ")
	handler.waitForReleases(t, 1)
}

// TestServeStopsOnContextCancel covers the cancellation contract.
func TestServeStopsOnContextCancel(t *testing.T) {
	handler := &fakeHandler{}
	server := NewServer(handler, "127.0.0.1", 0)
	logs := &logSink{}
	server.logOutput = logs

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- server.Serve(ctx) }()
	address := listeningAddress(t, logs)
	if server.Address() != address {
		t.Fatalf("Address() = %q, want %q", server.Address(), address)
	}

	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve after cancel = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after cancellation")
	}

	if err := server.Stop(); err != nil {
		t.Fatalf("Stop after Serve: %v", err)
	}
	if conn, err := net.DialTimeout("tcp", address, 200*time.Millisecond); err == nil {
		conn.Close()
		t.Fatal("the port still accepts connections after Serve returned")
	}
}

// TestStopReturnsServe covers Stop as the shutdown trigger, including its
// idempotence.
func TestStopReturnsServe(t *testing.T) {
	handler := &fakeHandler{}
	server, _, _ := startServer(t, handler)

	if err := server.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := server.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	// The cleanup registered by startServer asserts Serve returned nil.
}

// TestServeRejectsBadSetup covers the two configuration errors.
func TestServeRejectsBadSetup(t *testing.T) {
	empty := NewServer(nil, "127.0.0.1", 0)
	if err := empty.Serve(context.Background()); err == nil {
		t.Fatal("Serve with a nil handler did not fail")
	}

	handler := &fakeHandler{}
	server, _, _ := startServer(t, handler)
	if err := server.Serve(context.Background()); err == nil {
		t.Fatal("a second concurrent Serve did not fail")
	} else if !strings.Contains(err.Error(), "already serving") {
		t.Fatalf("second Serve error = %v", err)
	}
}

// TestAddressBeforeServe reports the configured address.
func TestAddressBeforeServe(t *testing.T) {
	server := NewServer(&fakeHandler{}, "127.0.0.1", 8765)
	if got := server.Address(); got != "127.0.0.1:8765" {
		t.Fatalf("Address() = %q", got)
	}
	wildcard := NewServer(&fakeHandler{}, "", 8765)
	if got := wildcard.Address(); got != ":8765" {
		t.Fatalf("wildcard Address() = %q", got)
	}
}

// TestLongLineClosesTheConnection pins the 64 KiB line limit inherited from
// Python's StreamReader.
func TestLongLineClosesTheConnection(t *testing.T) {
	handler := &fakeHandler{}
	_, logs, address := startServer(t, handler)
	conn, reader := dial(t, address)
	logs.waitFor(t, "[ipc] bridge connected: ")

	// The server may reset the connection while this oversized write is still
	// in flight, which is exactly the failure mode under test.
	_, _ = conn.Write([]byte(strings.Repeat("x", maxLineBytes+1024) + "\n"))
	waitForEOF(t, conn, reader)
	handler.waitForReleases(t, 1)
}

// TestFinalLineWithoutNewlineIsHandled mirrors readline returning the last
// partial line when the peer half-closes the stream.
func TestFinalLineWithoutNewlineIsHandled(t *testing.T) {
	handler := &fakeHandler{reply: func(map[string]any) (map[string]any, error) {
		return map[string]any{"type": "ack"}, nil
	}}
	_, _, address := startServer(t, handler)
	conn, reader := dial(t, address)

	if _, err := conn.Write([]byte(`{"type":"hello"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		t.Fatalf("connection is %T, want *net.TCPConn", conn)
	}
	if err := tcp.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}

	frame := readJSON(t, reader)
	if frame["type"] != "ack" {
		t.Fatalf("frame = %+v", frame)
	}
	if got := handler.messageCount(); got != 1 {
		t.Fatalf("handled messages = %d, want 1", got)
	}
}

// TestBridgeCanReconnect covers single-writer replacement and a fresh release
// per disconnect.
func TestBridgeCanReconnect(t *testing.T) {
	handler := &fakeHandler{}
	server, logs, address := startServer(t, handler)

	first, _ := dial(t, address)
	waitForBridges(t, logs, 1)
	first.Close()
	handler.waitForReleases(t, 1)

	second, secondReader := dial(t, address)
	waitForBridges(t, logs, 2)
	if err := server.Send("npc", map[string]any{"type": "after_reconnect"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	frame := readJSON(t, secondReader)
	if frame["type"] != "after_reconnect" {
		t.Fatalf("frame = %+v", frame)
	}

	second.Close()
	handler.waitForReleases(t, 2)
	logs.waitFor(t, "[ipc] bridge disconnected: ")
}

// TestSendRejectsNilPayload keeps the one input error of Send explicit.
func TestSendRejectsNilPayload(t *testing.T) {
	server := NewServer(&fakeHandler{}, "127.0.0.1", 0)
	if err := server.Send("npc", nil); err == nil {
		t.Fatal("Send(nil) did not fail")
	}
}

// TestMessagesFromTwoBridgesAreHandledConcurrently documents that each
// connection gets its own goroutine.
func TestMessagesFromTwoBridgesAreHandledConcurrently(t *testing.T) {
	handler := &fakeHandler{handleDelay: 150 * time.Millisecond}
	_, logs, address := startServer(t, handler)

	first, _ := dial(t, address)
	second, _ := dial(t, address)
	waitForBridges(t, logs, 2)

	start := time.Now()
	sendJSON(t, first, map[string]any{"type": "one"})
	sendJSON(t, second, map[string]any{"type": "two"})
	handler.waitForReleases(t, 0)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && handler.messageCount() < 2 {
		time.Sleep(2 * time.Millisecond)
	}
	if got := handler.messageCount(); got != 2 {
		t.Fatalf("handled messages = %d, want 2", got)
	}
	if elapsed := time.Since(start); elapsed >= 250*time.Millisecond {
		t.Fatalf("two connections took %v, want them handled in parallel", elapsed)
	}
}
