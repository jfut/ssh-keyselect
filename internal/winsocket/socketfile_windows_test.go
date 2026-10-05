//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package winsocket

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDialCancellationInterruptsHandshake(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	path := filepath.Join(t.TempDir(), "agent.sock")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("!<socket >%d s 00000000-00000000-00000000-00000000",
		listener.Addr().(*net.TCPAddr).Port)), 0600); err != nil {
		t.Fatal(err)
	}
	peer := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			peer <- nil
			return
		}
		var token [16]byte
		if _, err := io.ReadFull(conn, token[:]); err != nil {
			_ = conn.Close()
			peer <- nil
			return
		}
		peer <- conn
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		conn, err := Dial(ctx, path)
		if conn != nil {
			_ = conn.Close()
		}
		result <- err
	}()
	select {
	case conn := <-peer:
		if conn == nil {
			t.Fatal("client did not start the handshake")
		}
		t.Cleanup(func() { _ = conn.Close() })
	case <-time.After(2 * time.Second):
		t.Fatal("client did not send the handshake token")
	}
	// Withhold the server response and cancel a context that has no deadline.
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("a canceled handshake succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceling Dial left a handshake blocked")
	}
}

func TestSocketMetadataRejectsOversizedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.sock")
	// Trimming an unbounded read would otherwise accept this valid header with excess padding.
	contents := append([]byte("!<socket >1234 s 00000000-00000000-00000000-00000000"), bytes.Repeat([]byte(" "), 1024)...)
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := read(path); err == nil {
		t.Fatal("oversized socket metadata was accepted")
	}
}
