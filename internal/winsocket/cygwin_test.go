// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package winsocket

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestCygwinSilentPeerDoesNotBlockOtherConnections(t *testing.T) {
	t.Parallel()
	listener := newCygwinTestListener(t)
	_ = dialCygwinTestPeer(t, listener)
	silent := acceptCygwinTestConnection(t, listener)
	blocked := make(chan error, 1)
	go func() {
		var payload [1]byte
		_, err := silent.Read(payload[:])
		blocked <- err
	}()

	exchangeCygwinTestPayload(t, listener, 0)
	_ = silent.Close()
	requireCygwinTestError(t, blocked)
}

func TestCygwinHandshakeLimitReleasesClosedConnections(t *testing.T) {
	t.Parallel()
	listener := newCygwinTestListener(t)
	listener.handshakeLimit = 1
	_ = dialCygwinTestPeer(t, listener)
	pending := acceptCygwinTestConnection(t, listener)
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		accepted <- conn
	}()
	excess := dialCygwinTestPeer(t, listener)
	requireCygwinTestPeerClosed(t, excess)

	_ = pending.Close()
	peer := dialCygwinTestPeer(t, listener)
	select {
	case conn := <-accepted:
		if conn == nil {
			t.Fatal("connection was not admitted after a handshake slot was released")
		}
		t.Cleanup(func() { _ = conn.Close() })
		exchangeCygwinTestConnection(t, listener.guid, peer, conn, 0)
	case <-time.After(2 * time.Second):
		t.Fatal("connection was blocked after a handshake slot was released")
	}
}

func TestCygwinHandshakeExpiresBeforeFirstRead(t *testing.T) {
	t.Parallel()
	listener := newCygwinTestListener(t)
	listener.handshakeLimit = 1
	listener.handshakeTimeout = time.Second
	peer := dialCygwinTestPeer(t, listener)
	_ = acceptCygwinTestConnection(t, listener)
	// Accepted connections must expire even if no handler starts reading them.
	requireCygwinTestPeerClosed(t, peer)
	exchangeCygwinTestPayload(t, listener, 0)
}

func TestCygwinAuthenticatedConnectionOutlivesHandshakeDeadline(t *testing.T) {
	t.Parallel()
	listener := newCygwinTestListener(t)
	listener.handshakeTimeout = time.Second
	// An authenticated connection may wait for a signing approval longer than the handshake timeout.
	exchangeCygwinTestPayload(t, listener, listener.handshakeTimeout+100*time.Millisecond)
}

func TestCygwinRejectsAgentTrafficBeforeAuthentication(t *testing.T) {
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			listener := newCygwinTestListener(t)
			listener.handshakeLimit = 1
			peer := dialCygwinTestPeer(t, listener)
			conn := acceptCygwinTestConnection(t, listener)
			result := make(chan error, 1)
			go func() {
				var buffer [1]byte
				var n int
				var err error
				if operation == "read" {
					n, err = conn.Read(buffer[:])
				} else {
					n, err = conn.Write([]byte("private agent response"))
				}
				if n != 0 {
					err = nil
				}
				result <- err
			}()
			invalid := listener.guid
			invalid[0] ^= 1
			// Include queued agent data so a failed handshake cannot expose the remaining payload.
			if _, err := peer.Write(append(invalid[:], []byte("agent payload")...)); err != nil {
				t.Fatal(err)
			}
			requireCygwinTestError(t, result)
			requireCygwinTestPeerClosed(t, peer)
			exchangeCygwinTestPayload(t, listener, 0)
		})
	}
}

func TestCygwinListenerCloseInterruptsPendingHandshakes(t *testing.T) {
	t.Parallel()
	listener := newCygwinTestListener(t)
	peers := []net.Conn{dialCygwinTestPeer(t, listener)}
	connections := []net.Conn{acceptCygwinTestConnection(t, listener)}
	peers = append(peers, dialCygwinTestPeer(t, listener))
	connections = append(connections, acceptCygwinTestConnection(t, listener))
	blocked := make(chan error, len(connections))
	for _, conn := range connections {
		go func() {
			var payload [1]byte
			_, err := conn.Read(payload[:])
			blocked <- err
		}()
	}
	if _, err := peers[1].Write(listener.guid[:]); err != nil {
		t.Fatal(err)
	}
	var token [16]byte
	if _, err := io.ReadFull(peers[1], token[:]); err != nil || token != listener.guid {
		t.Fatalf("server handshake = %x, %v; want the socket token", token, err)
	}
	// Cover both a silent peer and a peer stalled after the token echo.
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	for _, peer := range peers {
		requireCygwinTestError(t, blocked)
		requireCygwinTestPeerClosed(t, peer)
	}
	if _, err := listener.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Accept after close = %v, want a closed listener", err)
	}
}

func newCygwinTestListener(t *testing.T) *cygwinListener {
	t.Helper()
	tcpListener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	listener := newCygwinListener(tcpListener, [16]byte{1, 2, 3, 4}, nil)
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func dialCygwinTestPeer(t *testing.T, listener net.Listener) net.Conn {
	t.Helper()
	peer, err := net.DialTimeout("tcp4", listener.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	if err := peer.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return peer
}

func acceptCygwinTestConnection(t *testing.T, listener net.Listener) net.Conn {
	t.Helper()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		accepted <- conn
	}()
	select {
	case conn := <-accepted:
		if conn == nil {
			t.Fatal("listener did not accept a connection")
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	case <-time.After(2 * time.Second):
		t.Fatal("handshake blocked TCP admission")
		return nil
	}
}

func exchangeCygwinTestPayload(t *testing.T, listener *cygwinListener, delay time.Duration) {
	t.Helper()
	peer := dialCygwinTestPeer(t, listener)
	conn := acceptCygwinTestConnection(t, listener)
	exchangeCygwinTestConnection(t, listener.guid, peer, conn, delay)
}

// Exchange the wire handshake independently from the production client, then echo an agent byte.
func exchangeCygwinTestConnection(t *testing.T, guid [16]byte, peer, conn net.Conn, delay time.Duration) {
	t.Helper()
	result := make(chan error, 1)
	go func() {
		var payload [1]byte
		_, err := io.ReadFull(conn, payload[:])
		if err == nil {
			_, err = conn.Write(payload[:])
		}
		result <- err
	}()
	if _, err := peer.Write(guid[:]); err != nil {
		t.Fatal(err)
	}
	var token [16]byte
	if _, err := io.ReadFull(peer, token[:]); err != nil || token != guid {
		t.Fatalf("server token = %x, %v; want %x", token, err, guid)
	}
	var info [12]byte
	if _, err := peer.Write(info[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(peer, info[:]); err != nil {
		t.Fatal(err)
	}
	time.Sleep(delay)
	if _, err := peer.Write([]byte{'x'}); err != nil {
		t.Fatal(err)
	}
	var echo [1]byte
	if _, err := io.ReadFull(peer, echo[:]); err != nil || echo[0] != 'x' {
		t.Fatalf("agent echo = %q, %v; want x", echo, err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent exchange did not complete")
	}
}

func requireCygwinTestError(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("unauthenticated agent operation succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unauthenticated agent operation remained blocked")
	}
}

func requireCygwinTestPeerClosed(t *testing.T, peer net.Conn) {
	t.Helper()
	var data [1]byte
	n, err := peer.Read(data[:])
	if n != 0 || err == nil {
		t.Fatalf("unauthenticated peer read = %x, %v; want a closed connection", data[:n], err)
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("unauthenticated connection was not closed")
	}
}
