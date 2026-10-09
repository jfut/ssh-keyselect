//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package listener

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Microsoft/go-winio"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/winpath"
	"github.com/jfut/ssh-keyselect/internal/winsocket"
)

const pipePrefix = `\\.\pipe\`

// ListenWithMode creates an agent listener using the selected client transport.
func ListenWithMode(path string, requested transport.Mode) (net.Listener, func(), error) {
	mode, err := transport.ParseMode(string(requested))
	if err != nil {
		return nil, nil, err
	}
	if mode == transport.Auto {
		mode, err = ResolveMode(path, "", mode)
		if err != nil {
			return nil, nil, err
		}
	}
	switch mode {
	case transport.Cygwin:
		if winpath.IsNamedPipe(path) {
			return nil, nil, errors.New("socket-file listen mode requires a filesystem path")
		}
		return winsocket.Listen(path)
	case transport.Unix, transport.WSL1:
		if winpath.IsNamedPipe(path) {
			return nil, nil, errors.New("unix-socket listen mode requires a filesystem path")
		}
		return listenUnixSocket(path)
	case transport.NamedPipe:
		if !winpath.IsNamedPipe(path) {
			return nil, nil, errors.New("named-pipe listen mode requires a named-pipe endpoint")
		}
	default:
		return nil, nil, fmt.Errorf("unsupported listen mode %q", mode)
	}
	name := strings.TrimPrefix(strings.ToLower(path), strings.ToLower(pipePrefix))
	if name == "" || strings.Contains(name, `\`) || name == "." || name == ".." {
		return nil, nil, errors.New("named-pipe endpoint must contain one pipe name")
	}
	ln, err := winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;SY)(A;;GA;;;OW)",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("listen on Windows named pipe: %w", err)
	}
	var once sync.Once
	cleanup := func() { once.Do(func() { _ = ln.Close() }) }
	return ln, cleanup, nil
}

// listenUnixSocket creates a Windows AF_UNIX socket under the user's filesystem path.
func listenUnixSocket(path string) (net.Listener, func(), error) {
	if path == "" {
		return nil, nil, errors.New("listen socket path is empty")
	}
	nativePath, err := winpath.NativeSocketPath(path)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve Windows Unix socket path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(nativePath), 0700); err != nil {
		return nil, nil, fmt.Errorf("create socket directory: %w", err)
	}
	ln, err := net.Listen("unix", nativePath)
	if err != nil {
		return nil, nil, fmt.Errorf("listen on Windows Unix socket: %w", err)
	}
	unixListener := ln.(*net.UnixListener)
	// net.UnixListener unlinks by path on close; disable that behavior so a replacement is preserved.
	unixListener.SetUnlinkOnClose(false)
	created, err := os.Lstat(nativePath)
	if err != nil {
		_ = ln.Close()
		return nil, nil, fmt.Errorf("inspect created Windows Unix socket: %w", err)
	}
	if created.Mode()&os.ModeSocket == 0 {
		_ = ln.Close()
		return nil, nil, fmt.Errorf("listen path was replaced during socket creation: %s", nativePath)
	}
	removeCreatedSocket := func() {
		current, err := os.Lstat(nativePath)
		if err == nil && os.SameFile(created, current) {
			_ = os.Remove(nativePath)
		}
	}
	cleanup := sync.OnceFunc(func() {
		_ = ln.Close()
		removeCreatedSocket()
	})
	return ln, cleanup, nil
}
