//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package upstream

import (
	"context"
	"net"
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

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := dialEndpoint(ctx, endpoint, transport.Auto)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	select {
	case acceptedConn := <-accepted:
		_ = acceptedConn.Close()
	case <-ctx.Done():
		t.Fatal("Git Bash socket connection was not accepted")
	}
}
