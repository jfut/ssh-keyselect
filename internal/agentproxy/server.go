// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package agentproxy implements the SSH agent frontend and per-connection authorization.
package agentproxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/protocol"
	"github.com/jfut/ssh-keyselect/internal/selector"
	"github.com/jfut/ssh-keyselect/internal/upstream"
)

const (
	// Bound frontend resources even when forwarded clients leave connections open.
	maxClientConnections    = 128
	clientFrameReadTimeout  = 10 * time.Second
	defaultSelectionTimeout = 120 * time.Second
)

var errSelectionTimeout = errors.New("identity selection timed out")

// Server serves SSH agent requests while isolating authorization state per client connection.
type Server struct {
	Agent            upstream.Agent
	Selector         selector.Selector
	Logger           *slog.Logger
	autoSelectMu     sync.RWMutex
	autoSelect       bool
	selectionTimeout atomic.Int64
}

// SetAutoSelect temporarily bypasses the picker and exposes every upstream identity.
func (s *Server) SetAutoSelect(enabled bool) {
	s.autoSelectMu.Lock()
	s.autoSelect = enabled
	s.autoSelectMu.Unlock()
}

// AutoSelect reports whether temporary pass-through mode is enabled.
func (s *Server) AutoSelect() bool {
	s.autoSelectMu.RLock()
	defer s.autoSelectMu.RUnlock()
	return s.autoSelect
}

// SetSelectionTimeout bounds how long an agent request can wait for a key choice.
func (s *Server) SetSelectionTimeout(timeout time.Duration) {
	if timeout <= 0 {
		timeout = defaultSelectionTimeout
	}
	s.selectionTimeout.Store(int64(timeout))
}

func (s *Server) selectionTimeoutDuration() time.Duration {
	if timeout := time.Duration(s.selectionTimeout.Load()); timeout > 0 {
		return timeout
	}
	return defaultSelectionTimeout
}

// Serve accepts frontend connections until ctx is cancelled or the listener fails.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	return s.serve(ctx, listener, maxClientConnections, clientFrameReadTimeout, upstream.RequestTimeout)
}

// serve lets tests use fewer connections and shorter deadlines without changing production limits.
func (s *Server) serve(ctx context.Context, listener net.Listener, connectionLimit int, frameReadTimeout, requestTimeout time.Duration) error {
	if s.Agent == nil || s.Selector == nil || listener == nil {
		return errors.New("agent proxy is not fully configured")
	}
	ctx, cancel := context.WithCancel(ctx)
	logger := s.logger()
	var mu sync.Mutex
	active := make(map[net.Conn]struct{})
	var handlers sync.WaitGroup
	shutdown := func() {
		_ = listener.Close()
		mu.Lock()
		for conn := range active {
			_ = conn.Close()
		}
		mu.Unlock()
	}
	stopShutdown := context.AfterFunc(ctx, shutdown)
	defer func() {
		// Listener failures must also cancel pickers and release every client.
		cancel()
		stopShutdown()
		shutdown()
		handlers.Wait()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept frontend connection: %w", err)
		}
		handshake, needsHandshake := conn.(interface{ Handshake() error })
		mu.Lock()
		if ctx.Err() != nil {
			mu.Unlock()
			_ = conn.Close()
			return nil
		}
		if len(active) >= connectionLimit {
			mu.Unlock()
			_ = conn.Close()
			// Unauthenticated loopback peers must not turn overload rejection into a log flood.
			if !needsHandshake {
				logger.Warn("rejected client connection at connection limit", "limit", connectionLimit)
			}
			continue
		}
		active[conn] = struct{}{}
		mu.Unlock()
		sessionID := newSessionID()
		handlers.Add(1)
		go func() {
			defer handlers.Done()
			defer func() {
				mu.Lock()
				delete(active, conn)
				mu.Unlock()
				_ = conn.Close()
			}()
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error("client handler recovered from panic", "session", sessionID)
				}
			}()
			// Count transport authentication in admission, without logging unauthenticated peers as clients.
			if needsHandshake {
				if err := handshake.Handshake(); err != nil {
					return
				}
			}
			s.handleConnection(ctx, conn, sessionID, logger, frameReadTimeout, requestTimeout)
		}()
	}
}

func (s *Server) handleConnection(parent context.Context, conn net.Conn, sessionID string, logger *slog.Logger, frameReadTimeout, requestTimeout time.Duration) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	log := logger.With("session", sessionID)
	log.Info("client connected")
	defer log.Info("client disconnected")

	packets := make(chan []byte, 1)
	readerDone := make(chan struct{})
	go func() {
		defer func() {
			// Every reader exit cancels the picker and unblocks response writes.
			cancel()
			_ = conn.Close()
			close(readerDone)
		}()
		for {
			message, err := readClientFrame(conn, frameReadTimeout)
			if err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && ctx.Err() == nil {
					log.Debug("client sent an invalid or incomplete agent frame")
				}
				return
			}
			select {
			case packets <- message:
			case <-ctx.Done():
				return
			default:
				// Never stop watching the client while a request is awaiting selection.
				log.Warn("rejected excessive pending agent requests")
				return
			}
		}
	}()
	defer func() {
		// Keep the connection slot occupied until its reader has also stopped.
		cancel()
		_ = conn.Close()
		<-readerDone
	}()

	// Cache the wire response so repeated requests do not copy or encode the selection again.
	var cachedResponse []byte
	selected := make(map[[sha256.Size]byte]struct{})
	var sessionBinds []verifiedSessionBind
	var upstreamSessionBinds [][]byte
	var sessionBindingBytes int
	upstreamSessionBindingRejected := false
	resetBindingChain := func() {
		sessionBinds = nil
		upstreamSessionBinds = nil
		sessionBindingBytes = 0
		upstreamSessionBindingRejected = false
		cachedResponse = nil
		clear(selected)
	}
	sessionBindRequests := 0
	for {
		select {
		case <-ctx.Done():
			return
		case message := <-packets:
			if len(message) == 0 {
				return
			}
			switch message[0] {
			case protocol.RequestIdentities:
				if len(message) != 1 {
					return
				}
				if cachedResponse == nil {
					available, err := listAgent(ctx, s.Agent, upstreamSessionBinds, requestTimeout)
					var chosen []identity.Identity
					if err == nil {
						selectionCtx, cancelSelection := s.newSelectionContext(ctx)
						chosen, err = s.selectAvailableIdentities(selectionCtx, log, selectionContext(sessionBinds), available)
						if errors.Is(selectionCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
							log.Warn("identity selection timed out", "timeout", s.selectionTimeoutDuration())
							err = errSelectionTimeout
						}
						cancelSelection()
					}
					if errors.Is(err, errSelectionTimeout) {
						// Let SSH continue as if no keys were selected, then release this timed-out client.
						emptyResponse, marshalErr := protocol.MarshalIdentities(nil)
						if marshalErr != nil || !writeResponse(conn, emptyResponse) {
							return
						}
						return
					}
					if err != nil {
						if ctx.Err() != nil {
							return
						}
						log.Warn("identity selection failed", "error", err)
						if !writeFailure(conn) {
							return
						}
						continue
					}
					cachedResponse, err = protocol.MarshalIdentities(chosen)
					if err != nil {
						return
					}
					for _, id := range chosen {
						selected[identity.Digest(id.Blob)] = struct{}{}
						log.Info("identity selected", "fingerprint", id.Fingerprint)
					}
				} else {
					log.Debug("returning cached identity selection", "identity_count", len(selected), "host_binding_count", len(sessionBinds))
				}
				if !writeResponse(conn, cachedResponse) {
					return
				}
			case protocol.ExtensionRequest:
				if sessionBindRequests >= maxSessionBindings {
					log.Warn("rejected excessive SSH session-binding requests")
					if !writeFailure(conn) {
						return
					}
					continue
				}
				sessionBindRequests++
				binding, err := verifySessionBind(message)
				if err != nil {
					log.Warn("rejected unsupported or invalid agent extension", "error", err)
					if !writeFailure(conn) {
						return
					}
					continue
				}
				authenticationBound := hasAuthenticationBinding(sessionBinds)
				if authenticationBound && binding.display.IsForwarding {
					// A new forwarded channel may repeat the forwarding path on this frontend socket.
					log.Info("starting a new SSH agent session after an authentication binding")
					resetBindingChain()
					authenticationBound = false
				}
				if hasSessionID(sessionBinds, binding.sessionID) {
					log.Warn("rejected duplicate SSH session binding")
					if !writeFailure(conn) {
						return
					}
					continue
				}
				if authenticationBound {
					// Authentication-bound agent connections cannot accept later bindings.
					log.Info("starting a new SSH agent session after an authentication binding")
					resetBindingChain()
				} else if cachedResponse != nil {
					// A new verified host context requires a fresh identity selection.
					cachedResponse = nil
					clear(selected)
				}
				log.Info("verified SSH host context for identity selection", "host_key", binding.display.Fingerprint, "forwarding", binding.display.IsForwarding)
				if upstreamSessionBindingRejected {
					if !writeFailure(conn) {
						return
					}
					continue
				}
				if len(binding.raw) > maxSessionBindingBytes-sessionBindingBytes {
					log.Warn("rejected excessive SSH session-binding data", "limit_bytes", maxSessionBindingBytes)
					if !writeFailure(conn) {
						return
					}
					continue
				}
				err = bindAgentSessionChain(ctx, s.Agent, upstreamSessionBinds, binding.raw, requestTimeout)
				if err != nil {
					upstreamSessionBindingRejected = true
					log.Warn("upstream agent rejected SSH session binding", "error", err)
					if !writeFailure(conn) {
						return
					}
					continue
				}
				binding.display.KnownHosts = sessionKnownHostNames(binding.hostKey)
				binding.hostKey = nil
				upstreamSessionBinds = append(upstreamSessionBinds, binding.raw)
				sessionBinds = append(sessionBinds, binding)
				sessionBindingBytes += len(binding.raw)
				if !writeResponse(conn, []byte{protocol.Success}) {
					return
				}
			case protocol.SignRequest:
				keyBlob, _, _, err := protocol.ParseSignRequest(message)
				if err != nil {
					return
				}
				digest := identity.Digest(keyBlob)
				if _, ok := selected[digest]; !ok {
					log.Warn("rejected signature request for an unselected identity", "fingerprint", identity.Fingerprint(keyBlob))
					if !writeFailure(conn) {
						return
					}
					continue
				}
				if upstreamSessionBindingRejected {
					if !writeFailure(conn) {
						return
					}
					continue
				}
				log.Info("signature requested", "fingerprint", identity.Fingerprint(keyBlob))
				response, err := roundTripAgent(ctx, s.Agent, upstreamSessionBinds, message, requestTimeout)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					log.Warn("upstream signature request failed", "error", err)
					if !writeFailure(conn) {
						return
					}
					continue
				}
				if !validSignResponse(response) {
					response = []byte{protocol.Failure}
				}
				if !writeResponse(conn, response) {
					return
				}
			default:
				// Reject management and unknown requests to keep the proxy read/sign-only.
				log.Warn("rejected unsupported agent request", "request_type", message[0])
				if !writeFailure(conn) {
					return
				}
				continue
			}
		}
	}
}

func readClientFrame(conn net.Conn, timeout time.Duration) ([]byte, error) {
	// Idle sockets may wait for user selection or later forwarded SSH sessions.
	// Start one absolute deadline only after the client begins sending a frame.
	var first [1]byte
	if _, err := io.ReadFull(conn, first[:]); err != nil {
		return nil, err
	}
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	message, err := protocol.ReadFrame(io.MultiReader(bytes.NewReader(first[:]), conn))
	if err != nil {
		return nil, err
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return nil, err
	}
	return message, nil
}

func (s *Server) selectAvailableIdentities(ctx context.Context, logger *slog.Logger, requestContext selector.SelectionContext, identities []identity.Identity) ([]identity.Identity, error) {
	logger.Info("identities requested", "upstream_count", len(identities))
	if len(identities) == 0 {
		return nil, nil
	}
	if s.AutoSelect() {
		logger.Info("auto select enabled; exposing all upstream identities", "identity_count", len(identities))
		return identities, nil
	}
	chosen, err := s.Selector.Select(ctx, identities, requestContext)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, errSelectionTimeout
	}
	if err != nil {
		if errors.Is(err, selector.ErrCancelled) {
			logger.Info("identity selection cancelled")
			return nil, nil
		}
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err := validateSelection(identities, chosen); err != nil {
		return nil, err
	}
	return chosen, nil
}

// newSelectionContext bounds the time spent waiting for a key choice.
func (s *Server) newSelectionContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, s.selectionTimeoutDuration())
}

func selectionContext(bindings []verifiedSessionBind) selector.SelectionContext {
	requestContext := selector.SelectionContext{HostBindings: make([]selector.HostBinding, len(bindings))}
	for i, binding := range bindings {
		requestContext.HostBindings[i] = binding.display
		requestContext.HostBindings[i].KnownHosts = append([]string(nil), binding.display.KnownHosts...)
	}
	return requestContext
}

func hasAuthenticationBinding(bindings []verifiedSessionBind) bool {
	for _, binding := range bindings {
		if !binding.display.IsForwarding {
			return true
		}
	}
	return false
}

func hasSessionID(bindings []verifiedSessionBind, sessionID []byte) bool {
	for _, binding := range bindings {
		if bytes.Equal(binding.sessionID, sessionID) {
			return true
		}
	}
	return false
}

// Bound operations replay their verified chain on short-lived connections so
// an interactive picker never leaves an upstream agent socket idle.
func listAgent(ctx context.Context, agent upstream.Agent, bindings [][]byte, requestTimeout time.Duration) ([]identity.Identity, error) {
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if len(bindings) == 0 {
		return agent.List(requestCtx)
	}
	session, err := openBoundSession(requestCtx, agent, bindings)
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()
	return session.List(requestCtx)
}

func bindAgentSessionChain(ctx context.Context, agent upstream.Agent, bindings [][]byte, binding []byte, requestTimeout time.Duration) error {
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	session, err := openBoundSession(requestCtx, agent, bindings)
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	return session.Bind(requestCtx, binding)
}

// Connection setup and binding replay have a deadline, while signing approval follows the client's lifetime.
func roundTripAgent(ctx context.Context, agent upstream.Agent, bindings [][]byte, request []byte, requestTimeout time.Duration) ([]byte, error) {
	setupCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	session, err := openBoundSession(setupCtx, agent, bindings)
	cancel()
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()
	return session.RoundTrip(ctx, request)
}

func openBoundSession(ctx context.Context, agent upstream.Agent, bindings [][]byte) (upstream.AgentSession, error) {
	session, err := agent.OpenSession(ctx)
	if err != nil {
		return nil, err
	}
	for i, binding := range bindings {
		if err := session.Bind(ctx, binding); err != nil {
			_ = session.Close()
			return nil, fmt.Errorf("restore SSH session binding %d: %w", i+1, err)
		}
	}
	return session, nil
}

func validateSelection(available, chosen []identity.Identity) error {
	if len(chosen) > len(available) {
		return errors.New("selector returned more identities than the upstream agent")
	}
	allowed := make(map[[sha256.Size]byte]struct{}, len(available))
	for _, id := range available {
		allowed[identity.Digest(id.Blob)] = struct{}{}
	}
	seen := make(map[[sha256.Size]byte]struct{}, len(chosen))
	for _, id := range chosen {
		digest := identity.Digest(id.Blob)
		if _, ok := allowed[digest]; !ok {
			return errors.New("selector returned an identity not offered by the upstream agent")
		}
		if _, ok := seen[digest]; ok {
			return errors.New("selector returned a duplicate identity")
		}
		seen[digest] = struct{}{}
	}
	return nil
}

func validSignResponse(message []byte) bool {
	if len(message) == 1 && message[0] == protocol.Failure {
		return true
	}
	if len(message) < 5 || message[0] != protocol.SignResponse {
		return false
	}
	n := uint64(message[1])<<24 | uint64(message[2])<<16 | uint64(message[3])<<8 | uint64(message[4])
	return n == uint64(len(message)-5)
}

func writeFailure(conn net.Conn) bool { return writeResponse(conn, []byte{protocol.Failure}) }

func writeResponse(conn net.Conn, response []byte) bool {
	return protocol.WriteFrame(conn, response) == nil
}

func newSessionID() string {
	var raw [6]byte
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}

func (s *Server) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
