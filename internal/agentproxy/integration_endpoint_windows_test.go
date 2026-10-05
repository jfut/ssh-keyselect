//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package agentproxy

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/jfut/ssh-keyselect/internal/listener"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/upstream"
	"github.com/jfut/ssh-keyselect/internal/winsocket"
)

func newIntegrationEndpoint(t *testing.T, name string) *integrationEndpoint {
	t.Helper()
	path := fmt.Sprintf(`\\.\pipe\ssh-keyselect-test-%d-%s`, os.Getpid(), name)
	ln, cleanup, err := listener.ListenWithMode(path, transport.NamedPipe)
	if err != nil {
		t.Fatal(err)
	}
	return &integrationEndpoint{
		path: path, mode: transport.NamedPipe, listener: ln, cleanup: cleanup,
		dial: func() (net.Conn, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return winio.DialPipeContext(ctx, path)
		},
	}
}

func TestCygwinProxySigning(t *testing.T) {
	t.Run("concurrent", func(t *testing.T) {
		testProxyConcurrentSigning(t, newCygwinIntegrationEndpoint)
	})
	t.Run("approval", func(t *testing.T) {
		testProxySigningWaitsForApprovalAndCancelsOnClientClose(t, newCygwinIntegrationEndpoint)
	})
}

func TestCygwinProxyCountsUnauthenticatedConnectionsWithoutLoggingThem(t *testing.T) {
	frontend := newCygwinIntegrationEndpoint(t, "pending-handshakes")
	t.Cleanup(frontend.cleanup)
	ctx, cancel := context.WithCancel(t.Context())
	var logs bytes.Buffer
	proxy := &Server{
		Agent: &fakeAgent{}, Selector: &selecting{},
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	}
	done := make(chan error, 1)
	go func() {
		done <- proxy.serve(ctx, frontend.listener, 1, clientFrameReadTimeout, upstream.RequestTimeout)
	}()
	t.Cleanup(func() {
		cancel()
		frontend.cleanup()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("proxy Serve: %v", err)
			}
			if logs.Len() != 0 {
				t.Errorf("unauthenticated peers generated client logs: %s", logs.String())
			}
		case <-time.After(2 * time.Second):
			t.Error("proxy did not release pending authentication")
		}
	})
	dialRaw := func() net.Conn {
		t.Helper()
		conn, err := net.DialTimeout("tcp4", frontend.listener.Addr().String(), 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}
		return conn
	}
	pending := dialRaw()
	excess := dialRaw()
	// A silent connection already occupies the only proxy slot, before authentication finishes.
	requireProxyClientClosed(t, excess)
	if _, err := pending.Write(make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	// The failed token and the earlier overload must both close without creating unauthenticated client logs.
	requireProxyClientClosed(t, pending)
}

func newCygwinIntegrationEndpoint(t *testing.T, name string) *integrationEndpoint {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".sock")
	ln, cleanup, err := listener.ListenWithMode(path, transport.Cygwin)
	if err != nil {
		t.Fatal(err)
	}
	return &integrationEndpoint{
		path: path, mode: transport.Cygwin, listener: ln, cleanup: cleanup,
		dial: func() (net.Conn, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return winsocket.Dial(ctx, path)
		},
	}
}
