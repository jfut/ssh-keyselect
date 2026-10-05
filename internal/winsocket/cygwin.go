// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package winsocket implements Windows-compatible agent socket-file transports.
package winsocket

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

const (
	maxPendingHandshakes   = 128
	cygwinHandshakeTimeout = 10 * time.Second
)

type cygwinListener struct {
	listener         *net.TCPListener
	guid             [16]byte
	cleanup          func()
	handshakeLimit   int
	handshakeTimeout time.Duration
	closeOnce        sync.Once
	closeErr         error
	mu               sync.Mutex
	closed           bool
	handshakes       map[*cygwinConn]struct{}
}

func newCygwinListener(listener *net.TCPListener, guid [16]byte, cleanup func()) *cygwinListener {
	return &cygwinListener{
		listener: listener, guid: guid, cleanup: cleanup,
		handshakeLimit: maxPendingHandshakes, handshakeTimeout: cygwinHandshakeTimeout,
		handshakes: make(map[*cygwinConn]struct{}),
	}
}

func (l *cygwinListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.listener.AcceptTCP()
		if err != nil {
			return nil, err
		}
		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			_ = conn.Close()
			return nil, net.ErrClosed
		}
		if len(l.handshakes) >= l.handshakeLimit {
			l.mu.Unlock()
			_ = conn.Close()
			continue
		}
		client := &cygwinConn{Conn: conn, listener: l}
		l.handshakes[client] = struct{}{}
		// Bound authentication from admission, even if the caller has not started reading.
		client.timer = time.AfterFunc(l.handshakeTimeout, client.expireHandshake)
		l.mu.Unlock()
		// Authenticate per connection so a silent peer cannot block Accept.
		// The proxy also counts this connection against its overall client limit.
		return client, nil
	}
}

func (l *cygwinListener) Close() error {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		pending := make([]*cygwinConn, 0, len(l.handshakes))
		for conn := range l.handshakes {
			pending = append(pending, conn)
		}
		l.mu.Unlock()
		l.closeErr = l.listener.Close()
		// Close outside the listener lock because each connection releases its handshake slot.
		for _, conn := range pending {
			_ = conn.Close()
		}
		if l.cleanup != nil {
			l.cleanup()
		}
	})
	return l.closeErr
}

func (l *cygwinListener) Addr() net.Addr { return l.listener.Addr() }

// cygwinConn keeps agent traffic behind authentication while preserving net.Conn concurrency.
type cygwinConn struct {
	net.Conn
	listener     *cygwinListener
	timer        *time.Timer
	handshake    sync.Once
	handshakeErr error
}

func (c *cygwinConn) Read(buffer []byte) (int, error) {
	if err := c.Handshake(); err != nil {
		return 0, err
	}
	return c.Conn.Read(buffer)
}

func (c *cygwinConn) Write(buffer []byte) (int, error) {
	if err := c.Handshake(); err != nil {
		return 0, err
	}
	return c.Conn.Write(buffer)
}

// Handshake lets the proxy authenticate after admission and before logging or handling agent requests.
// Read and Write also enforce it for other listener callers.
func (c *cygwinConn) Handshake() error {
	c.handshake.Do(func() {
		c.handshakeErr = cygwinServerHandshake(c.Conn, c.listener.guid)
		if c.handshakeErr == nil {
			l := c.listener
			l.mu.Lock()
			if _, pending := l.handshakes[c]; !pending || l.closed {
				c.handshakeErr = net.ErrClosed
			} else {
				delete(l.handshakes, c)
				c.timer.Stop()
			}
			l.mu.Unlock()
		}
		if c.handshakeErr != nil {
			_ = c.Close()
		}
	})
	return c.handshakeErr
}

func (c *cygwinConn) expireHandshake() {
	l := c.listener
	l.mu.Lock()
	_, pending := l.handshakes[c]
	delete(l.handshakes, c)
	l.mu.Unlock()
	// Check under the same lock as authentication: an expired timer must never close an authenticated signer.
	if pending {
		_ = c.Conn.Close()
	}
}

func (c *cygwinConn) Close() error {
	l := c.listener
	l.mu.Lock()
	delete(l.handshakes, c)
	c.timer.Stop()
	l.mu.Unlock()
	return c.Conn.Close()
}

func cygwinServerHandshake(conn net.Conn, guid [16]byte) error {
	var challenge [16]byte
	if _, err := io.ReadFull(conn, challenge[:]); err != nil {
		return fmt.Errorf("read Cygwin socket handshake: %w", err)
	}
	if challenge != guid {
		return fmt.Errorf("cygwin socket handshake GUID did not match")
	}
	if err := writeAll(conn, guid[:]); err != nil {
		return fmt.Errorf("write Cygwin socket handshake: %w", err)
	}
	var clientInfo [12]byte
	if _, err := io.ReadFull(conn, clientInfo[:]); err != nil {
		return fmt.Errorf("read Cygwin client information: %w", err)
	}
	binary.LittleEndian.PutUint32(clientInfo[:4], uint32(os.Getpid()))
	if err := writeAll(conn, clientInfo[:]); err != nil {
		return fmt.Errorf("write Cygwin client information: %w", err)
	}
	return nil
}

func writeAll(conn net.Conn, data []byte) error {
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
