// Package ipc implements the newline-delimited JSON IPC server the RDR2 bridge
// connects to.
//
// It is a port of `runtime/ipc/server.py`. The Python runtime is the server;
// the C++ ASI bridge connects to localhost and exchanges one JSON object per
// line. Each connection is served by its own goroutine and every write is
// serialised, so the dispatcher may push actions from any goroutine.
package ipc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
)

// maxLineBytes mirrors the 64 KiB line limit of Python's asyncio StreamReader.
// A longer line ends the connection, exactly like the Python LimitOverrunError.
const maxLineBytes = 64 * 1024

// disconnectReason is the release reason the Python server passes when the
// bridge goes away.
const disconnectReason = "bridge_disconnect"

// Handler consumes bridge messages. It is the local stand-in for
// `NpcAgentRuntime` so this package does not depend on the agent package.
type Handler interface {
	// HandleMessage processes one decoded bridge message and returns an
	// optional reply payload. A nil reply writes nothing back, mirroring
	// `handle_message` returning None. An error ends the connection.
	HandleMessage(ctx context.Context, raw map[string]any) (map[string]any, error)
	// ReleaseAll releases every temporary AI ownership back to Rockstar AI.
	// It runs once per bridge disconnect, after the bridge is detached, with
	// the reason string the Python server passes ("bridge_disconnect").
	ReleaseAll(ctx context.Context, reason string) error
}

// Server is the newline-delimited JSON server.
//
// It keeps one "current bridge" connection, like the Python server's single
// `_writer`: a newly connected bridge replaces the previous one as the
// target of Send, and only the current bridge's disconnect triggers
// ReleaseAll.
type Server struct {
	handler Handler
	host    string
	port    int

	mu       sync.Mutex
	listener net.Listener
	conn     net.Conn
	addr     string
	serving  bool

	// writeMu serialises writes to a connection. Python's event loop provided
	// that serialisation implicitly.
	writeMu sync.Mutex

	// logMu guards logOutput, which defaults to os.Stdout and is replaced in
	// tests.
	logMu     sync.Mutex
	logOutput io.Writer
}

// NewServer builds a server for one handler. The socket is not bound until
// Serve runs, so a port of 0 selects a free port that Address reports.
func NewServer(handler Handler, host string, port int) *Server {
	return &Server{handler: handler, host: host, port: port, logOutput: os.Stdout}
}

// Address returns the bound host:port, or the configured one before Serve has
// bound the socket. After Serve returns it reports the last bound address.
func (s *Server) Address() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.addr != "" {
		return s.addr
	}
	return s.listenAddr()
}

// Serve binds the socket and serves connections until ctx is cancelled or Stop
// is called, then returns nil. It blocks.
func (s *Server) Serve(ctx context.Context) error {
	if s.handler == nil {
		return errors.New("ipc: server has no handler")
	}

	s.mu.Lock()
	if s.serving {
		s.mu.Unlock()
		return errors.New("ipc: server is already serving")
	}
	s.serving = true
	s.mu.Unlock()

	listener, err := net.Listen("tcp", s.listenAddr())
	if err != nil {
		s.mu.Lock()
		s.serving = false
		s.mu.Unlock()
		return fmt.Errorf("ipc: listen on %s: %w", s.listenAddr(), err)
	}

	s.mu.Lock()
	s.listener = listener
	s.addr = listener.Addr().String()
	s.mu.Unlock()

	// done lets the context watcher exit when Serve finishes for another
	// reason, so the goroutine does not outlive the call.
	done := make(chan struct{})
	defer func() {
		close(done)
		_ = listener.Close()
		s.mu.Lock()
		if s.listener == listener {
			s.listener = nil
		}
		s.serving = false
		s.mu.Unlock()
	}()

	go func() {
		select {
		case <-ctx.Done():
			// Closing the listener unblocks Accept and makes Serve return nil.
			_ = s.Stop()
		case <-done:
		}
	}()

	s.logf("[ipc] listening on %s", s.Address())

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("ipc: accept: %w", err)
		}
		go s.handleClient(ctx, conn)
	}
}

// Stop closes the listening socket. It is safe to call more than once, does
// not close the current bridge connection, and does not release ownership,
// mirroring BridgeServer.stop.
func (s *Server) Stop() error {
	s.mu.Lock()
	listener := s.listener
	s.listener = nil
	s.mu.Unlock()
	if listener == nil {
		return nil
	}
	return listener.Close()
}

// Send writes one payload to the connected bridge.
//
// The Python transport has a single bridge connection and multiplexes NPCs by
// the npc_id inside the payload, so npcID is informational here and is not
// injected into the payload. Send is a no-op returning nil when no bridge is
// connected, mirroring `send_json`; unlike Python, a failed write is reported
// to the caller instead of being dropped.
func (s *Server) Send(npcID string, payload map[string]any) error {
	if payload == nil {
		return errors.New("ipc: nil payload")
	}
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return nil
	}
	return s.writeTo(conn, payload)
}

// handleClient serves one bridge connection. Python's ordering is preserved:
// the connection becomes current, messages are handled one at a time (each
// handler call may block), and on disconnect the connection is detached before
// ReleaseAll runs, then it is closed, then the disconnect is reported.
func (s *Server) handleClient(ctx context.Context, conn net.Conn) {
	peer := conn.RemoteAddr().String()

	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()

	s.logf("[ipc] bridge connected: %s", peer)

	defer func() {
		s.mu.Lock()
		isCurrent := s.conn == conn
		if isCurrent {
			// Detach first: anything ReleaseAll emits is not written to a
			// dying socket.
			s.conn = nil
		}
		s.mu.Unlock()

		if isCurrent {
			// Spec release policy: a bridge disconnect releases all temporary
			// AI ownership back to Rockstar AI. The reason is the literal the
			// Python server passes.
			if err := s.handler.ReleaseAll(ctx, disconnectReason); err != nil {
				s.logErrorf("[ipc] release_all failed: %v", err)
			}
		}
		_ = conn.Close()
		s.logf("[ipc] bridge disconnected: %s", peer)
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 4096), maxLineBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		var decoded any
		if err := json.Unmarshal(line, &decoded); err != nil {
			if err := s.writeTo(conn, errorPayload("invalid json")); err != nil {
				return
			}
			continue
		}
		raw, ok := decoded.(map[string]any)
		if !ok {
			if err := s.writeTo(conn, errorPayload("message must be an object")); err != nil {
				return
			}
			continue
		}

		reply, err := s.handler.HandleMessage(ctx, raw)
		if err != nil {
			// Python lets a handler exception escape the connection loop,
			// which then runs the disconnect path.
			s.logErrorf("[ipc] handle_message failed: %v", err)
			return
		}
		if reply != nil {
			if err := s.writeTo(conn, reply); err != nil {
				return
			}
		}
	}
	if err := scanner.Err(); err != nil {
		s.logErrorf("[ipc] bridge read failed: %v", err)
	}
}

// writeTo serialises one payload onto a connection.
func (s *Server) writeTo(conn net.Conn, payload map[string]any) error {
	line, err := encodeLine(payload)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	written, err := conn.Write(line)
	if err != nil {
		return err
	}
	if written < len(line) {
		return io.ErrShortWrite
	}
	return nil
}

// listenAddr is the configured bind address.
func (s *Server) listenAddr() string {
	return net.JoinHostPort(s.host, strconv.Itoa(s.port))
}

// logf writes one status line and flushes it immediately. The three lines
// required by the Python runtime are produced here verbatim.
func (s *Server) logf(format string, args ...any) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if s.logOutput == nil {
		return
	}
	fmt.Fprintf(s.logOutput, format+"\n", args...)
}

// logErrorf reports a failure the way Python's asyncio loop reports an
// unhandled task exception: on stderr, so the protocol lines on stdout stay
// parseable.
func (s *Server) logErrorf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// errorPayload builds the protocol error frame, mirroring `_write`.
func errorPayload(reason string) map[string]any {
	return map[string]any{"type": "error", "reason": reason}
}

// encodeLine marshals one protocol frame exactly like Python's
// `json.dumps(payload, ensure_ascii=False, separators=(",", ":")) + "\n"`:
// compact, raw UTF-8, and no HTML escaping.
func encodeLine(payload map[string]any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return nil, fmt.Errorf("ipc: encode payload: %w", err)
	}
	return buffer.Bytes(), nil
}
