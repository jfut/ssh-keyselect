// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package upstream

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/protocol"
	"github.com/jfut/ssh-keyselect/internal/transport"
)

func TestAgentSessionKeepsBindingsAndRequestsOnOneConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	bindings := [][]byte{{protocol.ExtensionRequest, 1}, {protocol.ExtensionRequest, 2}}
	identityRequest := []byte{protocol.RequestIdentities}
	signRequest := []byte{protocol.SignRequest, 7}
	serverErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer func() { _ = conn.Close() }()
		for _, binding := range bindings {
			got, err := protocol.ReadFrame(conn)
			if err != nil {
				serverErr <- err
				return
			}
			if string(got) != string(binding) {
				serverErr <- fmt.Errorf("unexpected upstream agent request %x, want %x", got, binding)
				return
			}
			if err := protocol.WriteFrame(conn, []byte{protocol.Success}); err != nil {
				serverErr <- err
				return
			}
		}
		for _, step := range []struct {
			request  []byte
			response []byte
		}{
			{request: identityRequest, response: []byte{protocol.IdentitiesAnswer, 0, 0, 0, 0}},
			{request: signRequest, response: []byte{protocol.Failure}},
		} {
			got, err := protocol.ReadFrame(conn)
			if err != nil {
				serverErr <- err
				return
			}
			if string(got) != string(step.request) {
				serverErr <- fmt.Errorf("unexpected upstream agent request %x, want %x", got, step.request)
				return
			}
			if err := protocol.WriteFrame(conn, step.response); err != nil {
				serverErr <- err
				return
			}
		}
		serverErr <- nil
	}()

	session, err := (EndpointAgent{Path: path, Mode: transport.Unix}).OpenSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	for _, binding := range bindings {
		if err := session.Bind(context.Background(), binding); err != nil {
			t.Fatal(err)
		}
	}
	identities, err := session.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(identities) != 0 {
		t.Fatalf("upstream identities = %d, want none", len(identities))
	}
	response, err := session.RoundTrip(context.Background(), signRequest)
	if err != nil {
		t.Fatal(err)
	}
	if len(response) != 1 || response[0] != protocol.Failure {
		t.Fatalf("agent response = %x, want SSH_AGENT_FAILURE", response)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}
