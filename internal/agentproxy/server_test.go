// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package agentproxy

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/protocol"
	"github.com/jfut/ssh-keyselect/internal/selector"
	"github.com/jfut/ssh-keyselect/internal/upstream"
)

func TestOnlySelectedIdentityCanSign(t *testing.T) {
	ids := []identity.Identity{proxyTestIdentity("alpha"), proxyTestIdentity("beta")}
	agent := &fakeAgent{identities: ids}
	_, client, cleanup := newPipeSession(t, agent, &selecting{index: 1})
	defer cleanup()
	selected, err := protocol.ParseIdentities(request(t, client, []byte{protocol.RequestIdentities}))
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].Comment != "beta" {
		t.Fatalf("identities returned = %+v, want beta only", selected)
	}

	signRequest, err := protocol.MarshalSignRequest(ids[0].Blob, []byte("challenge"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := request(t, client, signRequest); len(got) != 1 || got[0] != protocol.Failure {
		t.Fatalf("unselected sign response = %x, want SSH_AGENT_FAILURE", got)
	}
	if got := agent.requestCount(); got != 0 {
		t.Fatalf("unselected signing request reached upstream %d times", got)
	}

	signRequest, err = protocol.MarshalSignRequest(ids[1].Blob, []byte("challenge"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := request(t, client, signRequest); len(got) != 8 || got[0] != protocol.SignResponse {
		t.Fatalf("selected sign response = %x, want SSH_AGENT_SIGN_RESPONSE", got)
	}
	if got := agent.requestCount(); got != 1 {
		t.Fatalf("selected signing request reached upstream %d times, want 1", got)
	}
}

func TestAutoSelectExposesAndAllowsEveryUpstreamIdentity(t *testing.T) {
	ids := []identity.Identity{proxyTestIdentity("alpha"), proxyTestIdentity("beta")}
	agent := &fakeAgent{identities: ids}
	chooser := &selecting{index: 0}
	server, client, cleanup := newPipeSession(t, agent, chooser)
	defer cleanup()
	server.SetAutoSelect(true)

	selected, err := protocol.ParseIdentities(request(t, client, []byte{protocol.RequestIdentities}))
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != len(ids) || selected[0].Comment != "alpha" || selected[1].Comment != "beta" {
		t.Fatalf("auto-selected identities = %+v, want all upstream identities", selected)
	}
	if got := chooser.calls.Load(); got != 0 {
		t.Fatalf("selector calls = %d, want no picker while Auto Select is enabled", got)
	}

	for _, id := range ids {
		signRequest, err := protocol.MarshalSignRequest(id.Blob, []byte("challenge"), 0)
		if err != nil {
			t.Fatal(err)
		}
		if response := request(t, client, signRequest); len(response) != 8 || response[0] != protocol.SignResponse {
			t.Fatalf("auto-selected signing response = %x, want SSH_AGENT_SIGN_RESPONSE", response)
		}
	}
	if got := agent.requestCount(); got != len(ids) {
		t.Fatalf("upstream signing requests = %d, want %d", got, len(ids))
	}
}

func TestManagementAndUnknownRequestsAreAlwaysRejected(t *testing.T) {
	agent := &fakeAgent{identities: []identity.Identity{proxyTestIdentity("alpha")}}
	_, client, cleanup := newPipeSession(t, agent, &selecting{index: 0})
	defer cleanup()

	requests := []struct {
		name    string
		message []byte
	}{
		{name: "add identity", message: []byte{protocol.AddIdentity}},
		{name: "unsupported extension", message: []byte{protocol.ExtensionRequest, 0, 0, 0, 3, 'x', 'y', 'z'}},
		{name: "unknown", message: []byte{0xff}},
	}
	for _, test := range requests {
		t.Run(test.name, func(t *testing.T) {
			if response := request(t, client, test.message); len(response) != 1 || response[0] != protocol.Failure {
				t.Errorf("request %x response = %x, want SSH_AGENT_FAILURE", test.message, response)
			}
		})
	}
	if got := agent.requestCount(); got != 0 {
		t.Fatalf("rejected requests reached upstream %d times, want 0", got)
	}
}

func TestCancellationReturnsAnEmptyIdentityAnswer(t *testing.T) {
	agent := &fakeAgent{identities: []identity.Identity{proxyTestIdentity("alpha")}}
	_, client, cleanup := newPipeSession(t, agent, &selecting{err: selector.ErrCancelled})
	defer cleanup()
	response := request(t, client, []byte{protocol.RequestIdentities})
	identities, err := protocol.ParseIdentities(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(identities) != 0 {
		t.Fatalf("cancel response contains %d identities", len(identities))
	}
}

func TestSelectionIsCachedForTheConnection(t *testing.T) {
	agent := &fakeAgent{identities: []identity.Identity{proxyTestIdentity("alpha")}}
	chooser := &selecting{index: 0}
	_, client, cleanup := newPipeSession(t, agent, chooser)
	defer cleanup()
	for i := 0; i < 2; i++ {
		response := request(t, client, []byte{protocol.RequestIdentities})
		if _, err := protocol.ParseIdentities(response); err != nil {
			t.Fatal(err)
		}
	}
	if got := chooser.calls.Load(); got != 1 {
		t.Fatalf("selector calls = %d, want one", got)
	}
}

func TestBoundAgentSessionClosesForPickerAndReplaysForSigning(t *testing.T) {
	ids := []identity.Identity{proxyTestIdentity("alpha")}
	agent := &fakeAgent{identities: ids}
	chooser := &blockingSelecting{started: make(chan struct{}), release: make(chan struct{})}
	_, client, cleanup := newPipeSession(t, agent, chooser)
	defer cleanup()

	binding, _ := testSessionBindMessage(t, []byte("bound session"), false)
	if response := request(t, client, binding); len(response) != 1 || response[0] != protocol.Success {
		t.Fatalf("session-bind response = %x, want SSH_AGENT_SUCCESS", response)
	}
	if err := protocol.WriteFrame(client, []byte{protocol.RequestIdentities}); err != nil {
		t.Fatal(err)
	}
	type responseResult struct {
		message []byte
		err     error
	}
	responseCh := make(chan responseResult, 1)
	go func() {
		message, err := protocol.ReadFrame(client)
		responseCh <- responseResult{message: message, err: err}
	}()
	select {
	case <-chooser.started:
	case <-time.After(time.Second):
		t.Fatal("identity picker did not open")
	}
	if !agent.allSessionsClosed() {
		t.Fatal("an upstream session remained open while the picker was waiting")
	}
	close(chooser.release)
	select {
	case result := <-responseCh:
		if result.err != nil {
			t.Fatalf("read selected identities: %v", result.err)
		}
		selected, err := protocol.ParseIdentities(result.message)
		if err != nil || len(selected) != 1 || selected[0].Comment != "alpha" {
			t.Fatalf("selected identities = %+v, %v; want alpha", selected, err)
		}
	case <-time.After(time.Second):
		t.Fatal("identity selection response did not arrive")
	}

	signRequest, err := protocol.MarshalSignRequest(ids[0].Blob, []byte("challenge"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if response := request(t, client, signRequest); len(response) != 8 || response[0] != protocol.SignResponse {
		t.Fatalf("sign response after picker wait = %x, want SSH_AGENT_SIGN_RESPONSE", response)
	}
	if got := agent.lastSignBindings(); len(got) != 1 || string(got[0]) != string(binding) {
		t.Fatalf("signing session bindings = %x, want the verified binding replayed", got)
	}
	if !agent.allSessionsClosed() {
		t.Fatal("an upstream session remained open after signing")
	}
}

func TestForwardingAfterAuthenticationStartsANewSelectionSession(t *testing.T) {
	agent := &fakeAgent{identities: []identity.Identity{proxyTestIdentity("alpha")}}
	chooser := &contextualSelecting{index: 0}
	_, client, cleanup := newPipeSession(t, agent, chooser)
	defer cleanup()

	firstForwardingBind, firstForwardingHostKey := testSessionBindMessage(t, []byte("first forwarding session"), true)
	firstAuthenticationBind, _ := testSessionBindMessage(t, []byte("first authentication session"), false)
	for _, binding := range [][]byte{firstForwardingBind, firstAuthenticationBind} {
		if response := request(t, client, binding); len(response) != 1 || response[0] != protocol.Success {
			t.Fatalf("initial session-bind response = %x, want SSH_AGENT_SUCCESS", response)
		}
	}
	if response := request(t, client, []byte{protocol.RequestIdentities}); len(response) == 0 || response[0] != protocol.IdentitiesAnswer {
		t.Fatalf("identities response = %x, want SSH_AGENT_IDENTITIES_ANSWER", response)
	}

	requestContext := chooser.selectionContext()
	if len(requestContext.HostBindings) != 2 {
		t.Fatalf("initial selection host bindings = %+v, want two verified bindings", requestContext.HostBindings)
	}
	forwardingBinding := requestContext.HostBindings[0]
	if forwardingBinding.Algorithm != firstForwardingHostKey.Type() || forwardingBinding.Fingerprint == "" || !forwardingBinding.IsForwarding {
		t.Fatalf("selection forwarding binding = %+v", forwardingBinding)
	}
	firstFingerprint := forwardingBinding.Fingerprint
	if got := agent.lastListBindings(); len(got) != 2 || string(got[0]) != string(firstForwardingBind) || string(got[1]) != string(firstAuthenticationBind) {
		t.Fatalf("initial upstream identity-list bindings = %x, want the first forwarding and authentication bindings", got)
	}

	signRequest, err := protocol.MarshalSignRequest(agent.identities[0].Blob, []byte("first challenge"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if response := request(t, client, signRequest); len(response) != 8 || response[0] != protocol.SignResponse {
		t.Fatalf("initial selected signing response = %x, want signature", response)
	}
	if got := agent.lastSignBindings(); len(got) != 2 || string(got[0]) != string(firstForwardingBind) || string(got[1]) != string(firstAuthenticationBind) {
		t.Fatalf("initial upstream signing bindings = %x, want the first chain", got)
	}

	secondForwardingBind, secondForwardingHostKey := testSessionBindMessage(t, []byte("second forwarding session"), true)
	secondAuthenticationBind, _ := testSessionBindMessage(t, []byte("second authentication session"), false)
	for _, binding := range [][]byte{firstForwardingBind, secondForwardingBind, secondAuthenticationBind} {
		if response := request(t, client, binding); len(response) != 1 || response[0] != protocol.Success {
			t.Fatalf("second-hop session-bind response = %x, want SSH_AGENT_SUCCESS", response)
		}
	}
	if response := request(t, client, []byte{protocol.RequestIdentities}); len(response) == 0 || response[0] != protocol.IdentitiesAnswer {
		t.Fatalf("second-hop identities response = %x, want SSH_AGENT_IDENTITIES_ANSWER", response)
	}
	requestContext = chooser.selectionContext()
	if len(requestContext.HostBindings) != 3 || requestContext.HostBindings[0].Fingerprint != firstFingerprint || requestContext.HostBindings[1].Algorithm != secondForwardingHostKey.Type() || requestContext.HostBindings[1].Fingerprint == firstFingerprint || !requestContext.HostBindings[1].IsForwarding || requestContext.HostBindings[2].IsForwarding {
		t.Fatalf("second-hop selection host bindings = %+v, want the repeated forwarding path and second destination", requestContext.HostBindings)
	}
	if got := chooser.calls.Load(); got != 2 {
		t.Fatalf("selector calls after the second-hop binding = %d, want two", got)
	}
	if got := agent.lastListBindings(); len(got) != 3 || string(got[0]) != string(firstForwardingBind) || string(got[1]) != string(secondForwardingBind) || string(got[2]) != string(secondAuthenticationBind) {
		t.Fatalf("second-hop upstream identity-list bindings = %x, want the second chain", got)
	}

	signRequest, err = protocol.MarshalSignRequest(agent.identities[0].Blob, []byte("second challenge"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if response := request(t, client, signRequest); len(response) != 8 || response[0] != protocol.SignResponse {
		t.Fatalf("second-hop selected signing response = %x, want signature", response)
	}
	if got := agent.lastSignBindings(); len(got) != 3 || string(got[0]) != string(firstForwardingBind) || string(got[1]) != string(secondForwardingBind) || string(got[2]) != string(secondAuthenticationBind) {
		t.Fatalf("second-hop upstream signing bindings = %x, want the second chain", got)
	}
}

func TestConcurrentSessionsDoNotShareSelectedKeys(t *testing.T) {
	ids := []identity.Identity{proxyTestIdentity("alpha"), proxyTestIdentity("beta")}
	agent := &fakeAgent{identities: ids}
	_, clientA, cleanupA := newPipeSession(t, agent, &selecting{index: 0})
	defer cleanupA()
	_, clientB, cleanupB := newPipeSession(t, agent, &selecting{index: 1})
	defer cleanupB()
	_ = request(t, clientA, []byte{protocol.RequestIdentities})
	_ = request(t, clientB, []byte{protocol.RequestIdentities})

	signB, err := protocol.MarshalSignRequest(ids[1].Blob, []byte("challenge"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if response := request(t, clientA, signB); len(response) != 1 || response[0] != protocol.Failure {
		t.Fatalf("session A sign response = %x, want failure", response)
	}
	if response := request(t, clientB, signB); len(response) != 8 || response[0] != protocol.SignResponse {
		t.Fatalf("session B sign response = %x, want signature", response)
	}
	if got := agent.requestCount(); got != 1 {
		t.Fatalf("upstream signing requests = %d, want only session B's request", got)
	}
}

func newPipeSession(t *testing.T, agent *fakeAgent, chooser selector.Selector) (*Server, net.Conn, func()) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	server := &Server{
		Agent:    agent,
		Selector: chooser,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = serverConn.Close() }()
		server.handleConnection(ctx, serverConn, "test", server.Logger)
	}()
	cleanup := func() {
		cancel()
		_ = clientConn.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("proxy connection handler did not stop")
		}
	}
	return server, clientConn, cleanup
}

func request(t *testing.T, conn net.Conn, payload []byte) []byte {
	t.Helper()
	if err := protocol.WriteFrame(conn, payload); err != nil {
		t.Fatal(err)
	}
	response, err := protocol.ReadFrame(conn)
	if err != nil {
		t.Fatalf("read agent response: %v", err)
	}
	return response
}

// selecting is shared with the platform integration tests to choose a fixed identity.
type selecting struct {
	index int
	// err is used by server_test.go when exercising picker errors.
	err error
	// calls is inspected only by server_test.go's concurrency cases.
	calls atomic.Int32
}

type blockingSelecting struct {
	started chan struct{}
	release chan struct{}
}

func (s *blockingSelecting) Select(ctx context.Context, identities []identity.Identity) ([]identity.Identity, error) {
	close(s.started)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.release:
		return []identity.Identity{identities[0]}, nil
	}
}

func (s *selecting) Select(_ context.Context, identities []identity.Identity) ([]identity.Identity, error) {
	s.calls.Add(1)
	if s.err != nil {
		return nil, s.err
	}
	return []identity.Identity{identities[s.index]}, nil
}

type contextualSelecting struct {
	index int
	mu    sync.Mutex
	ctx   selector.SelectionContext
	calls atomic.Int32
}

func (s *contextualSelecting) Select(ctx context.Context, identities []identity.Identity) ([]identity.Identity, error) {
	return (&selecting{index: s.index}).Select(ctx, identities)
}

func (s *contextualSelecting) SelectWithContext(ctx context.Context, identities []identity.Identity, requestContext selector.SelectionContext) ([]identity.Identity, error) {
	s.calls.Add(1)
	s.mu.Lock()
	s.ctx = requestContext
	s.mu.Unlock()
	return s.Select(ctx, identities)
}

func (s *contextualSelecting) selectionContext() selector.SelectionContext {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctx
}

type fakeAgent struct {
	identities   []identity.Identity
	mu           sync.Mutex
	requests     [][]byte
	listBindings [][]byte
	signBindings [][]byte
	sessions     []*fakeAgentSession
}

func (f *fakeAgent) List(context.Context) ([]identity.Identity, error) {
	return cloneIdentities(f.identities), nil
}

func (f *fakeAgent) OpenSession(context.Context) (upstream.AgentSession, error) {
	session := &fakeAgentSession{agent: f}
	f.mu.Lock()
	f.sessions = append(f.sessions, session)
	f.mu.Unlock()
	return session, nil
}

type fakeAgentSession struct {
	agent     *fakeAgent
	mu        sync.Mutex
	bindings  [][]byte
	authBound bool
	closed    bool
}

func (s *fakeAgentSession) Bind(_ context.Context, binding []byte) error {
	extension, err := protocol.ParseExtensionRequest(binding)
	if err != nil {
		return err
	}
	parsed, err := protocol.ParseSessionBindRequest(extension.Payload)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.authBound {
		return errors.New("agent connection is already bound for authentication")
	}
	s.bindings = append(s.bindings, append([]byte(nil), binding...))
	if !parsed.IsForwarding {
		s.authBound = true
	}
	return nil
}

func (s *fakeAgentSession) List(ctx context.Context) ([]identity.Identity, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("agent session is closed")
	}
	bindings := cloneByteSlices(s.bindings)
	s.mu.Unlock()
	s.agent.mu.Lock()
	s.agent.listBindings = bindings
	s.agent.mu.Unlock()
	return s.agent.List(ctx)
}

func (s *fakeAgentSession) RoundTrip(ctx context.Context, request []byte) ([]byte, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("agent session is closed")
	}
	bindings := cloneByteSlices(s.bindings)
	s.mu.Unlock()
	s.agent.mu.Lock()
	s.agent.signBindings = bindings
	s.agent.mu.Unlock()
	return s.agent.RoundTrip(ctx, request)
}

func (s *fakeAgentSession) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

func (f *fakeAgent) RoundTrip(_ context.Context, request []byte) ([]byte, error) {
	f.mu.Lock()
	f.requests = append(f.requests, append([]byte(nil), request...))
	f.mu.Unlock()
	if len(request) > 0 && request[0] == protocol.SignRequest {
		return []byte{protocol.SignResponse, 0, 0, 0, 3, 's', 'i', 'g'}, nil
	}
	return []byte{protocol.Failure}, nil
}

func (f *fakeAgent) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeAgent) lastListBindings() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneByteSlices(f.listBindings)
}

func (f *fakeAgent) lastSignBindings() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneByteSlices(f.signBindings)
}

func (f *fakeAgent) allSessionsClosed() bool {
	f.mu.Lock()
	sessions := append([]*fakeAgentSession(nil), f.sessions...)
	f.mu.Unlock()
	for _, session := range sessions {
		session.mu.Lock()
		closed := session.closed
		session.mu.Unlock()
		if !closed {
			return false
		}
	}
	return true
}

func cloneByteSlices(values [][]byte) [][]byte {
	clone := make([][]byte, len(values))
	for i := range values {
		clone[i] = append([]byte(nil), values[i]...)
	}
	return clone
}

func proxyTestIdentity(comment string) identity.Identity {
	algorithm := []byte("ssh-ed25519")
	keyData := []byte(comment)
	blob := make([]byte, 8+len(algorithm)+len(keyData))
	binary.BigEndian.PutUint32(blob[:4], uint32(len(algorithm)))
	copy(blob[4:], algorithm)
	keyOffset := 4 + len(algorithm)
	binary.BigEndian.PutUint32(blob[keyOffset:keyOffset+4], uint32(len(keyData)))
	copy(blob[keyOffset+4:], keyData)
	id, err := identity.New(blob, []byte(comment))
	if err != nil {
		panic(err)
	}
	return id
}
