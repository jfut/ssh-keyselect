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
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/jfut/ssh-keyselect/internal/winpath"
	"github.com/jfut/ssh-keyselect/internal/winsocket"
	"golang.org/x/sys/windows"
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
	if info, err := os.Lstat(nativePath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, nil, fmt.Errorf("listen path exists and is not a socket: %s", nativePath)
		}
		if err := winsocket.CheckCurrentUserFileOwner(nativePath); err != nil {
			return nil, nil, fmt.Errorf("stale listen socket is not owned by the current user: %w", err)
		}
		conn, dialErr := net.DialTimeout("unix", nativePath, 300*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return nil, nil, fmt.Errorf("another instance is already listening on %s", nativePath)
		}
		if !errors.Is(dialErr, windows.WSAECONNREFUSED) && !errors.Is(dialErr, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("cannot determine whether socket is stale: %w", dialErr)
		}
		current, err := os.Lstat(nativePath)
		if errors.Is(err, os.ErrNotExist) {
			// Another process already removed the stale socket; bind below.
		} else if err != nil {
			return nil, nil, fmt.Errorf("reinspect stale socket: %w", err)
		} else if !os.SameFile(info, current) {
			return nil, nil, fmt.Errorf("listen path changed while checking stale socket: %s", nativePath)
		} else if err := os.Remove(nativePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("remove stale socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("inspect listen socket: %w", err)
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
