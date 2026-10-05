// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package agentproxy

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/protocol"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/upstream"
)

// Each test server uses a shorter setup deadline so approval waits can exceed it without slowing the suite.
const approvalTestRequestTimeout = 200 * time.Millisecond

func TestProxySigningWaitsForApprovalAndCancelsOnClientClose(t *testing.T) {
	testProxySigningWaitsForApprovalAndCancelsOnClientClose(t, newIntegrationEndpoint)
}

func testProxySigningWaitsForApprovalAndCancelsOnClientClose(t *testing.T, endpointFactory func(*testing.T, string) *integrationEndpoint) {
	t.Helper()
	for _, name := range []string{"unbound", "bound"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			identities := []identity.Identity{proxyTestIdentity("approval")}
			identityResponse, err := protocol.MarshalIdentities(identities)
			if err != nil {
				t.Fatal(err)
			}
			signature := testSignResponse([]byte("challenge"))
			frontendEndpoint, signing, approve := startApprovalTestProxy(t, "signing-"+name, identityResponse, endpointFactory)
			client, err := frontendEndpoint.dial()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = client.Close() }()
			if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			if name == "bound" {
				binding, _ := testSessionBindMessage(t, []byte("approval session"), false)
				if response := request(t, client, binding); !bytes.Equal(response, []byte{protocol.Success}) {
					t.Fatalf("session-bind response = %x, want success", response)
				}
			}
			if response := request(t, client, []byte{protocol.RequestIdentities}); !bytes.Equal(response, identityResponse) {
				t.Fatalf("identity response = %x, want the offered key", response)
			}
			nextSignature := func() net.Conn {
				t.Helper()
				select {
				case conn := <-signing:
					return conn
				case <-time.After(2 * time.Second):
					t.Fatal("signing request did not reach the upstream agent")
					return nil
				}
			}
			signRequest := testSignRequest(identities[0].Blob, []byte("challenge"), 0)
			if err := protocol.WriteFrame(client, signRequest); err != nil {
				t.Fatal(err)
			}
			nextSignature()
			// A confirmation prompt can outlive the setup deadline on a real socket or named pipe.
			time.Sleep(approvalTestRequestTimeout + 100*time.Millisecond)
			approve <- struct{}{}
			response, err := protocol.ReadFrame(client)
			if err != nil || !bytes.Equal(response, signature) {
				t.Fatalf("signature after a long approval wait = %x, %v; want %x", response, err, signature)
			}

			if err := protocol.WriteFrame(client, signRequest); err != nil {
				t.Fatal(err)
			}
			pending := nextSignature()
			// Withhold approval and require client disconnection to interrupt the upstream wait.
			if err := client.Close(); err != nil {
				t.Fatal(err)
			}
			if err := pending.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			requireProxyClientClosed(t, pending)
		})
	}
}

func TestProxyProcessesConcurrentSigningRequests(t *testing.T) {
	testProxyConcurrentSigning(t, newIntegrationEndpoint)
}

func testProxyConcurrentSigning(t *testing.T, endpointFactory func(*testing.T, string) *integrationEndpoint) {
	t.Helper()
	for _, name := range []string{"unbound", "bound"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			identities := []identity.Identity{proxyTestIdentity("concurrent")}
			identityResponse, err := protocol.MarshalIdentities(identities)
			if err != nil {
				t.Fatal(err)
			}
			frontend, signing, approve := startApprovalTestProxy(t, "concurrent-"+name, identityResponse, endpointFactory)
			if frontend.mode == transport.Cygwin {
				// Leave an earlier TCP peer unauthenticated while legitimate clients request signatures.
				peer, err := net.DialTimeout("tcp4", frontend.listener.Addr().String(), 2*time.Second)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = peer.Close() })
			}
			const clientCount = 4
			clients := make([]net.Conn, clientCount)
			for i := range clients {
				client, err := frontend.dial()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = client.Close() })
				if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
					t.Fatal(err)
				}
				if name == "bound" {
					binding, _ := testSessionBindMessage(t, []byte(fmt.Sprintf("session-%d", i)), false)
					if response := request(t, client, binding); !bytes.Equal(response, []byte{protocol.Success}) {
						t.Fatalf("client %d session-bind response = %x, want success", i, response)
					}
				}
				if response := request(t, client, []byte{protocol.RequestIdentities}); !bytes.Equal(response, identityResponse) {
					t.Fatalf("client %d identities = %x, want the selected key", i, response)
				}
				challenge := []byte(fmt.Sprintf("challenge-%d", i))
				if err := protocol.WriteFrame(client, testSignRequest(identities[0].Blob, challenge, 0)); err != nil {
					t.Fatal(err)
				}
				clients[i] = client
			}
			// Require every signature to reach the upstream before allowing any approval to finish.
			// A signing request waiting on one connection must not stall other clients.
			for range clients {
				select {
				case <-signing:
				case <-time.After(2 * time.Second):
					t.Fatal("concurrent signing requests did not reach the upstream together")
				}
			}
			close(approve)
			for i, client := range clients {
				response, err := protocol.ReadFrame(client)
				want := testSignResponse([]byte(fmt.Sprintf("challenge-%d", i)))
				if err != nil || !bytes.Equal(response, want) {
					t.Fatalf("client %d signature = %x, %v; want its own response %x", i, response, err, want)
				}
			}
		})
	}
}

// Start real frontend and upstream transports with approvals controlled by the test.
func startApprovalTestProxy(t *testing.T, name string, identities []byte,
	endpointFactory func(*testing.T, string) *integrationEndpoint) (*integrationEndpoint, <-chan net.Conn, chan<- struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	signing := make(chan net.Conn, 4)
	approve := make(chan struct{})
	upstreamEndpoint := endpointFactory(t, name+"-upstream")
	t.Cleanup(upstreamEndpoint.cleanup)
	upstreamDone := make(chan struct{})
	go serveApprovalTestUpstream(ctx, upstreamEndpoint.listener, identities, signing, approve, upstreamDone)
	frontendEndpoint := endpointFactory(t, name+"-frontend")
	t.Cleanup(frontendEndpoint.cleanup)
	proxy := &Server{
		Agent:    upstream.EndpointAgent{Path: upstreamEndpoint.path, Mode: upstreamEndpoint.mode},
		Selector: &selecting{index: 0},
	}
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- proxy.serve(ctx, frontendEndpoint.listener, maxClientConnections, clientFrameReadTimeout, approvalTestRequestTimeout)
	}()
	t.Cleanup(func() {
		cancel()
		frontendEndpoint.cleanup()
		upstreamEndpoint.cleanup()
		select {
		case err := <-serveDone:
			if err != nil {
				t.Errorf("proxy Serve: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("proxy did not stop after cancellation")
		}
		select {
		case <-upstreamDone:
		case <-time.After(2 * time.Second):
			t.Error("upstream did not release its connections")
		}
	})
	return frontendEndpoint, signing, approve
}

func testSignResponse(data []byte) []byte {
	return ssh.Marshal(struct {
		Type      byte
		Signature []byte
	}{protocol.SignResponse, data})
}

// Serve approval prompts on actual upstream connections, keeping binding replay and signing on the same connection.
func serveApprovalTestUpstream(ctx context.Context, listener net.Listener, identities []byte,
	signing chan<- net.Conn, approve <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	var handlers sync.WaitGroup
	defer handlers.Wait()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		handlers.Go(func() {
			defer func() { _ = conn.Close() }()
			stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
			defer stopClose()
			for {
				message, err := protocol.ReadFrame(conn)
				if err != nil || len(message) == 0 {
					return
				}
				var response []byte
				switch message[0] {
				case protocol.RequestIdentities:
					response = identities
				case protocol.ExtensionRequest:
					response = []byte{protocol.Success}
				case protocol.SignRequest:
					_, data, _, err := protocol.ParseSignRequest(message)
					if err != nil {
						return
					}
					select {
					case signing <- conn:
					case <-ctx.Done():
						return
					}
					select {
					case <-approve:
					case <-ctx.Done():
						return
					}
					// Distinct opaque signatures let clients detect responses delivered to the wrong connection.
					response = testSignResponse(data)
				default:
					response = []byte{protocol.Failure}
				}
				if err := protocol.WriteFrame(conn, response); err != nil {
					return
				}
			}
		})
	}
}
