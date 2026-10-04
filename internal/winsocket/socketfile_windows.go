//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package winsocket implements Windows-compatible agent socket-file transports.
package winsocket

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"

	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/winpath"
)

var cygwinSocketFilePattern = regexp.MustCompile(`^!<socket >([0-9]+) s ([0-9A-Fa-f]{8}(?:-[0-9A-Fa-f]{8}){3})$`)

// SocketFile holds the transport metadata stored in a Cygwin-compatible endpoint file.
type SocketFile struct {
	Mode     transport.Mode
	Port     uint16
	GUIDData [16]byte
}

// DetectMode identifies named pipes, compatibility socket files, or ordinary Unix sockets.
func DetectMode(path string) (transport.Mode, error) {
	if winpath.IsNamedPipe(path) {
		return transport.NamedPipe, nil
	}
	nativePath, err := winpath.NativeSocketPath(path)
	if err != nil {
		return transport.Unix, nil
	}
	contents, err := os.ReadFile(nativePath)
	if err != nil {
		return transport.Unix, nil
	}
	contents = trimSocketFile(contents)
	if !bytes.HasPrefix(contents, []byte("!<socket >")) {
		return transport.Unix, nil
	}
	info, err := parse(contents)
	if err != nil {
		return "", err
	}
	return info.Mode, nil
}

// read loads and parses transport metadata from a compatibility socket file.
func read(path string) (SocketFile, error) {
	nativePath, err := winpath.NativeSocketPath(path)
	if err != nil {
		return SocketFile{}, err
	}
	contents, err := os.ReadFile(nativePath)
	if err != nil {
		return SocketFile{}, err
	}
	return parse(trimSocketFile(contents))
}

// parse converts the text representation of a Cygwin-compatible socket file to metadata.
func parse(contents []byte) (SocketFile, error) {
	text := string(contents)
	if match := cygwinSocketFilePattern.FindStringSubmatch(text); match != nil {
		return makeSocketFile(match[1], match[2])
	}
	return SocketFile{}, fmt.Errorf("unrecognized compatibility socket file")
}

// Dial connects to a Cygwin-compatible socket and completes its handshake.
func Dial(ctx context.Context, path string, mode transport.Mode) (net.Conn, error) {
	info, err := read(path)
	if err != nil {
		return nil, fmt.Errorf("read compatible socket file: %w", err)
	}
	if info.Mode != mode {
		return nil, fmt.Errorf("mode %s does not match socket file mode %s", mode, info.Mode)
	}
	if mode != transport.Cygwin {
		return nil, fmt.Errorf("socket-file transport %q is not supported", mode)
	}
	conn, err := dialLoopbackTCP(ctx, info.Port, nil)
	if err != nil {
		return nil, err
	}
	if err := cygwinClientHandshake(ctx, conn, info.GUIDData); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// Listen publishes a Cygwin-compatible socket file and accepts agent connections.
func Listen(path string, mode transport.Mode) (net.Listener, func(), error) {
	if mode != transport.Cygwin {
		return nil, nil, fmt.Errorf("socket-file listener does not support mode %q", mode)
	}
	if path == "" {
		return nil, nil, fmt.Errorf("listen socket path is empty")
	}
	nativePath, err := winpath.NativeSocketPath(path)
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(nativePath), 0700); err != nil {
		return nil, nil, fmt.Errorf("create socket-file directory: %w", err)
	}
	tcpListener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, nil, fmt.Errorf("listen on loopback TCP socket: %w", err)
	}
	port := uint16(tcpListener.Addr().(*net.TCPAddr).Port)
	guidData, err := randomGUIDData()
	if err != nil {
		_ = tcpListener.Close()
		return nil, nil, err
	}
	guid := formatGUID(guidData)
	info := SocketFile{Mode: mode, Port: port, GUIDData: guidData}
	fileText := fmt.Sprintf("!<socket >%d s %s", port, guid)
	file, err := os.OpenFile(nativePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		_ = tcpListener.Close()
		return nil, nil, fmt.Errorf("create compatibility socket file: %w", err)
	}
	if _, err := io.WriteString(file, fileText); err != nil {
		_ = file.Close()
		_ = os.Remove(nativePath)
		_ = tcpListener.Close()
		return nil, nil, fmt.Errorf("write compatibility socket file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(nativePath)
		_ = tcpListener.Close()
		return nil, nil, fmt.Errorf("close compatibility socket file: %w", err)
	}
	if err := setSystemFileAttribute(nativePath); err != nil {
		_ = os.Remove(nativePath)
		_ = tcpListener.Close()
		return nil, nil, fmt.Errorf("mark compatibility socket file: %w", err)
	}
	fileInfo, err := os.Stat(nativePath)
	if err != nil {
		_ = os.Remove(nativePath)
		_ = tcpListener.Close()
		return nil, nil, fmt.Errorf("inspect compatibility socket file: %w", err)
	}
	ln := &compatListener{listener: tcpListener, path: nativePath, fileInfo: fileInfo, info: info}
	cleanup := ln.close
	return ln, cleanup, nil
}

type compatListener struct {
	listener  *net.TCPListener
	path      string
	fileInfo  os.FileInfo
	info      SocketFile
	closeOnce sync.Once
	closeErr  error
}

func (l *compatListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.listener.AcceptTCP()
		if err != nil {
			return nil, err
		}
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		err = cygwinServerHandshake(conn, l.info.GUIDData)
		if err != nil {
			_ = conn.Close()
			continue
		}
		_ = conn.SetDeadline(time.Time{})
		return conn, nil
	}
}

func (l *compatListener) Close() error {
	l.closeOnce.Do(func() {
		l.closeErr = l.listener.Close()
		if current, err := os.Stat(l.path); err == nil && os.SameFile(l.fileInfo, current) {
			_ = os.Remove(l.path)
		}
	})
	return l.closeErr
}

func (l *compatListener) Addr() net.Addr { return l.listener.Addr() }

func (l *compatListener) close() { _ = l.Close() }

func makeSocketFile(portText, guidText string) (SocketFile, error) {
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return SocketFile{}, fmt.Errorf("invalid compatibility socket port %q", portText)
	}
	guidBytes, err := parseGUID(guidText)
	if err != nil {
		return SocketFile{}, err
	}
	return SocketFile{Mode: transport.Cygwin, Port: uint16(port), GUIDData: guidBytes}, nil
}

func parseGUID(value string) ([16]byte, error) {
	var result [16]byte
	groups := strings.Split(value, "-")
	if len(groups) != 4 {
		return result, fmt.Errorf("invalid compatibility socket GUID")
	}
	for groupIndex, group := range groups {
		if len(group) != 8 {
			return result, fmt.Errorf("invalid compatibility socket GUID")
		}
		for byteIndex := 0; byteIndex < 4; byteIndex++ {
			decoded, err := strconv.ParseUint(group[byteIndex*2:byteIndex*2+2], 16, 8)
			if err != nil {
				return result, fmt.Errorf("invalid compatibility socket GUID: %w", err)
			}
			result[groupIndex*4+3-byteIndex] = byte(decoded)
		}
	}
	return result, nil
}

func randomGUIDData() ([16]byte, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return data, fmt.Errorf("generate compatibility socket GUID: %w", err)
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	return data, nil
}

func formatGUID(data [16]byte) string {
	return fmt.Sprintf("%08X-%08X-%08X-%08X",
		binary.LittleEndian.Uint32(data[0:4]),
		binary.LittleEndian.Uint32(data[4:8]),
		binary.LittleEndian.Uint32(data[8:12]),
		binary.LittleEndian.Uint32(data[12:16]),
	)
}

func trimSocketFile(contents []byte) []byte {
	return bytes.TrimSpace(bytes.TrimPrefix(contents, []byte{0xef, 0xbb, 0xbf}))
}

func cygwinClientHandshake(ctx context.Context, conn net.Conn, guid [16]byte) error {
	setContextDeadline(ctx, conn)
	if err := writeAll(conn, guid[:]); err != nil {
		return fmt.Errorf("write Cygwin socket handshake: %w", err)
	}
	var challenge [16]byte
	if _, err := io.ReadFull(conn, challenge[:]); err != nil {
		return fmt.Errorf("read Cygwin socket handshake: %w", err)
	}
	if challenge != guid {
		return fmt.Errorf("Cygwin socket handshake GUID did not match")
	}
	var clientInfo [12]byte
	binary.LittleEndian.PutUint32(clientInfo[:4], uint32(os.Getpid()))
	if err := writeAll(conn, clientInfo[:]); err != nil {
		return fmt.Errorf("write Cygwin client information: %w", err)
	}
	var serverInfo [12]byte
	if _, err := io.ReadFull(conn, serverInfo[:]); err != nil {
		return fmt.Errorf("read Cygwin client information: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	return nil
}

func cygwinServerHandshake(conn net.Conn, guid [16]byte) error {
	var challenge [16]byte
	if _, err := io.ReadFull(conn, challenge[:]); err != nil {
		return fmt.Errorf("read Cygwin socket handshake: %w", err)
	}
	if challenge != guid {
		return fmt.Errorf("Cygwin socket handshake GUID did not match")
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

func setSystemFileAttribute(path string) error {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.SetFileAttributes(pathPointer, windows.FILE_ATTRIBUTE_ARCHIVE|windows.FILE_ATTRIBUTE_SYSTEM)
}

func dialLoopbackTCP(ctx context.Context, port uint16, localAddress *net.TCPAddr) (net.Conn, error) {
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
	dialer := net.Dialer{LocalAddr: localAddress}
	conn, err := dialer.DialContext(ctx, "tcp4", address)
	if err != nil {
		return nil, fmt.Errorf("connect to loopback socket: %w", err)
	}
	return conn, nil
}

func setContextDeadline(ctx context.Context, conn net.Conn) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
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
