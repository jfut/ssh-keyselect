//go:build !windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package listener creates a private local endpoint for the agent frontend.
package listener

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jfut/ssh-keyselect/internal/transport"
)

// ListenWithMode starts the requested agent transport on Unix platforms.
func ListenWithMode(path string, requested transport.Mode) (net.Listener, func(), error) {
	mode, err := transport.ParseMode(string(requested))
	if err != nil {
		return nil, nil, err
	}
	if mode != transport.Auto && mode != transport.Unix && mode != transport.WSL1 {
		return nil, nil, fmt.Errorf("listen mode %q is supported only on Windows", mode)
	}
	if path == "" {
		return nil, nil, errors.New("listen socket path is empty")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve listen socket path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, nil, fmt.Errorf("create socket directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, nil, fmt.Errorf("listen path exists and is not a socket: %s", path)
		}
		conn, dialErr := net.DialTimeout("unix", path, 300*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return nil, nil, fmt.Errorf("another instance is already listening on %s", path)
		}
		if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, syscall.ENOENT) {
			return nil, nil, fmt.Errorf("cannot determine whether socket is stale: %w", dialErr)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("remove stale socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("inspect listen socket: %w", err)
	}

	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, nil, fmt.Errorf("listen on Unix socket: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = ln.Close()
		_ = os.Remove(path)
		return nil, nil, fmt.Errorf("set Unix socket permissions: %w", err)
	}
	created, err := os.Lstat(path)
	if err != nil {
		_ = ln.Close()
		_ = os.Remove(path)
		return nil, nil, fmt.Errorf("inspect created Unix socket: %w", err)
	}
	cleanup := func() {
		_ = ln.Close()
		current, err := os.Lstat(path)
		if err == nil && os.SameFile(created, current) {
			_ = os.Remove(path)
		}
	}
	return ln, cleanup, nil
}
