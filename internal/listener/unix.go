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
	"sync"
	"syscall"
	"time"

	"github.com/jfut/ssh-keyselect/internal/transport"
)

// ListenWithMode starts the requested agent transport on Unix platforms.
func ListenWithMode(path string, requested transport.Mode) (net.Listener, func(), error) {
	_, err := ResolveMode(path, "", requested)
	if err != nil {
		return nil, nil, err
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
		if err := checkSocketOwner(info, path); err != nil {
			return nil, nil, err
		}
		current, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			// Another process already removed the stale socket; bind below.
		} else if err != nil {
			return nil, nil, fmt.Errorf("reinspect stale socket: %w", err)
		} else if !os.SameFile(info, current) {
			return nil, nil, fmt.Errorf("listen path changed while checking stale socket: %s", path)
		} else if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("remove stale socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("inspect listen socket: %w", err)
	}

	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, nil, fmt.Errorf("listen on Unix socket: %w", err)
	}
	// Cleanup checks ownership before unlinking; net.UnixListener's automatic unlink would bypass that check.
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	created, err := os.Lstat(path)
	if err != nil {
		_ = ln.Close()
		return nil, nil, fmt.Errorf("inspect created Unix socket: %w", err)
	}
	if created.Mode()&os.ModeSocket == 0 {
		_ = ln.Close()
		return nil, nil, fmt.Errorf("listen path was replaced during socket creation: %s", path)
	}
	removeCreatedSocket := func() {
		current, err := os.Lstat(path)
		if err == nil && os.SameFile(created, current) {
			_ = os.Remove(path)
		}
	}
	if err := os.Chmod(path, 0600); err != nil {
		removeCreatedSocket()
		_ = ln.Close()
		return nil, nil, fmt.Errorf("set Unix socket permissions: %w", err)
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(created, current) {
		removeCreatedSocket()
		_ = ln.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("inspect created Unix socket: %w", err)
		}
		return nil, nil, fmt.Errorf("listen path changed while setting socket permissions: %s", path)
	}
	cleanup := sync.OnceFunc(func() {
		removeCreatedSocket()
		_ = ln.Close()
	})
	return ln, cleanup, nil
}

// checkSocketOwner avoids unlinking a stale endpoint owned by another Unix user.
func checkSocketOwner(info os.FileInfo, path string) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || uint64(stat.Uid) != uint64(os.Geteuid()) {
		return fmt.Errorf("stale listen socket is not owned by the current user: %s", path)
	}
	return nil
}
