// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package agentproxy

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/jfut/ssh-keyselect/internal/identity"
	"github.com/jfut/ssh-keyselect/internal/protocol"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/upstream"
)

type integrationEndpoint struct {
	path     string
	mode     transport.Mode
	listener net.Listener
	cleanup  func()
	dial     func() (net.Conn, error)
}

func TestProxyTransportFiltersIdentitiesAndForwardsSelectedSign(t *testing.T) {
	upstreamIdentities := []identity.Identity{proxyTestIdentity("first"), proxyTestIdentity("second"), proxyTestIdentity("third")}
	upstreamEndpoint := newIntegrationEndpoint(t, "upstream")
	defer upstreamEndpoint.cleanup()
	upstreamDone := make(chan struct{})
	go serveTestUpstream(upstreamEndpoint.listener, upstreamIdentities, upstreamDone)

	frontendEndpoint := newIntegrationEndpoint(t, "frontend")
	defer frontendEndpoint.cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proxy := &Server{
		Agent:    upstream.EndpointAgent{Path: upstreamEndpoint.path, Mode: upstreamEndpoint.mode},
		Selector: &selecting{index: 2},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- proxy.Serve(ctx, frontendEndpoint.listener) }()

	client, err := frontendEndpoint.dial()
	if err != nil {
		t.Fatal(err)
	}
	selected, err := protocol.ParseIdentities(request(t, client, []byte{protocol.RequestIdentities}))
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].Comment != "third" {
		t.Fatalf("frontend identities = %+v, want third only", selected)
	}
	signRequest, err := protocol.MarshalSignRequest(upstreamIdentities[2].Blob, []byte("challenge"), 0)
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, client, signRequest)
	if len(response) != 8 || response[0] != protocol.SignResponse {
		t.Fatalf("forwarded sign response = %x", response)
	}
	_ = client.Close()
	cancel()
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("proxy Serve: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy did not stop after cancellation")
	}
	upstreamEndpoint.cleanup()
	select {
	case <-upstreamDone:
	case <-time.After(time.Second):
		t.Fatal("fake upstream did not stop")
	}
}
