//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package upstream

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/jfut/ssh-keyselect/internal/listener"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/winpath"
)

func TestDialEndpointConnectsToGitBashSocket(t *testing.T) {
	nativePath := filepath.Join(t.TempDir(), "agent.sock")
	endpoint, err := winpath.ToGitBashPath(nativePath)
	if err != nil {
		t.Fatal(err)
	}
	ln, cleanup, err := listener.ListenWithMode(endpoint, transport.Cygwin)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	accepted := make(chan error, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			accepted <- acceptErr
			return
		}
		defer func() { _ = conn.Close() }()
		// The first I/O completes authentication and verifies the accepted connection carries agent data.
		var payload [1]byte
		_, err := io.ReadFull(conn, payload[:])
		if err == nil {
			_, err = conn.Write(payload[:])
		}
		accepted <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := dialEndpoint(ctx, endpoint, transport.Auto)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte{'x'}); err != nil {
		t.Fatal(err)
	}
	var response [1]byte
	if _, err := io.ReadFull(conn, response[:]); err != nil || response[0] != 'x' {
		t.Fatalf("Git Bash socket response = %q, %v; want x", response, err)
	}
	select {
	case err := <-accepted:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Git Bash socket connection was not accepted")
	}
}
