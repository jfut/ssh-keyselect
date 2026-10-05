// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package agentproxy

import (
	"bytes"
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/protocol"
	"github.com/jfut/ssh-keyselect/internal/upstream"
)

func TestProxySigningWaitsForApprovalAndCancelsOnClientClose(t *testing.T) {
	for _, name := range []string{"unbound", "bound"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			identities := []identity.Identity{proxyTestIdentity("approval")}
			identityResponse, err := protocol.MarshalIdentities(identities)
			if err != nil {
				t.Fatal(err)
			}
			signature := []byte{protocol.SignResponse, 0, 0, 0, 3, 's', 'i', 'g'}
			signing := make(chan net.Conn, 1)
			approve := make(chan struct{})
			upstreamEndpoint := newIntegrationEndpoint(t, "signing-upstream-"+name)
			t.Cleanup(upstreamEndpoint.cleanup)
			upstreamDone := make(chan struct{})
			go serveApprovalTestUpstream(ctx, upstreamEndpoint.listener, identityResponse, signature, signing, approve, upstreamDone)

			frontendEndpoint := newIntegrationEndpoint(t, "signing-frontend-"+name)
			t.Cleanup(frontendEndpoint.cleanup)
			proxy := &Server{
				Agent:    upstream.EndpointAgent{Path: upstreamEndpoint.path, Mode: upstreamEndpoint.mode},
				Selector: &selecting{index: 0},
			}
			serveDone := make(chan error, 1)
			go func() { serveDone <- proxy.Serve(ctx, frontendEndpoint.listener) }()
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
			client, err := frontendEndpoint.dial()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = client.Close() }()
			if err := client.SetDeadline(time.Now().Add(upstream.RequestTimeout + 5*time.Second)); err != nil {
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
			time.Sleep(upstream.RequestTimeout + 100*time.Millisecond)
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

// Serve approval prompts on actual upstream connections, keeping binding replay and signing on the same connection.
func serveApprovalTestUpstream(ctx context.Context, listener net.Listener, identities, signature []byte,
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
					response = signature
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
